// Package child_process is what the Go engine's rules about a child's piped output read: which local variable holds a
// pipe from exec.Cmd's StdoutPipe or StderrPipe, which command it belongs to, whether the function writes either again,
// and which calls wait for the child. correctness_require_drained_child_output asks whether the pipe is read before
// the wait, and concurrency_require_joined_pipe_reader whether a goroutine reading it is joined before the wait, and
// both name a pipe, a command and a wait the same way through here. It holds no rule itself.
//
// Every callee is resolved by its package and receiver, never by its spelling, so a method of the same name on any
// other type is never mistaken for exec.Cmd's.
package child_process

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/types/typeutil"
)

// Command names an exec.Cmd the way a function's code reaches it: the variable it starts from, and the fields walked
// from there, so cmd.StdoutPipe and cmd.Wait are the same command, and job.cmd.Wait another.
type Command struct {
	Root types.Object
	Path string
}

// Pipe is one pipe a rule follows: the local variable holding it, the stream it reads, the command whose output it
// is, and the graph node that makes it, at Index in Block once the caller has placed it.
type Pipe struct {
	Variable *types.Var
	Stream   string
	Command  Command
	Made     ast.Node
	Block    *cfg.Block
	Index    int
}

// PipeMade reads node as the declaration of a pipe: `stdout, err := cmd.StdoutPipe()`, or the same with var, whose
// first name the statement declares, of a command a later line can name. A pipe assigned to `_` is dropped where it
// is made, and a variable assigned with = may hold something else on another path, so neither is followed.
func PipeMade(pass *analysis.Pass, node ast.Node) (Pipe, bool) {
	var target *ast.Ident
	var value ast.Expr
	switch statement := node.(type) {
	case *ast.AssignStmt:
		if statement.Tok != token.DEFINE || len(statement.Lhs) == 0 || len(statement.Rhs) != 1 {
			return Pipe{}, false
		}
		identifier, isIdentifier := statement.Lhs[0].(*ast.Ident)
		if !isIdentifier {
			return Pipe{}, false
		}
		target = identifier
		value = statement.Rhs[0]
	case *ast.ValueSpec:
		if len(statement.Names) == 0 || len(statement.Values) != 1 {
			return Pipe{}, false
		}
		target = statement.Names[0]
		value = statement.Values[0]
	default:
		return Pipe{}, false
	}
	if target.Name == "_" {
		return Pipe{}, false
	}
	// A := that redeclares a variable already in scope defines nothing for it, so Defs holds no variable.
	variable, isVariable := pass.TypesInfo.Defs[target].(*types.Var)
	if !isVariable {
		return Pipe{}, false
	}
	call, isCall := ast.Unparen(value).(*ast.CallExpr)
	if !isCall {
		return Pipe{}, false
	}
	method, receiver := CommandMethod(pass, call)
	var stream string
	switch method {
	case "StdoutPipe":
		stream = "stdout"
	case "StderrPipe":
		stream = "stderr"
	default:
		return Pipe{}, false
	}
	named, isNamed := CommandOf(pass.TypesInfo, receiver)
	if !isNamed {
		return Pipe{}, false
	}
	return Pipe{Variable: variable, Stream: stream, Command: named, Made: node}, true
}

// Rewritten says body writes the pipe's variable anywhere besides its declaration, takes its address, or assigns the
// command's root variable or a field on the way to the command, so the variable may hold another reader or the
// command another child on some path. A write inside a function literal counts as much as one in the body, since
// the rule cannot place when the literal runs.
func Rewritten(pass *analysis.Pass, body *ast.BlockStmt, made Pipe) bool {
	found := false
	writes := func(target ast.Expr) {
		// A name this statement declares is a new variable, never a write to one the rule follows.
		if identifier, isIdentifier := ast.Unparen(target).(*ast.Ident); isIdentifier &&
			pass.TypesInfo.Defs[identifier] != nil {
			return
		}
		named, isNamed := CommandOf(pass.TypesInfo, target)
		if !isNamed {
			return
		}
		if named.Root == made.Variable {
			found = true
		}
		if named.Root == made.Command.Root &&
			(named.Path == made.Command.Path || strings.HasPrefix(made.Command.Path, named.Path+".")) {
			found = true
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for _, target := range node.Lhs {
				writes(target)
			}
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN {
				if node.Key != nil {
					writes(node.Key)
				}
				if node.Value != nil {
					writes(node.Value)
				}
			}
		case *ast.UnaryExpr:
			if identifier, isIdentifier := ast.Unparen(node.X).(*ast.Ident); isIdentifier && node.Op == token.AND &&
				pass.TypesInfo.Uses[identifier] == made.Variable {
				found = true
			}
		}
		return !found
	})
	return found
}

// Mentions says node names object anywhere in it, function literals included, so a goroutine that copies a pipe
// counts as reading it from where it is started. A defer statement never counts: its call runs on the way out,
// after any wait the function reaches first.
func Mentions(pass *analysis.Pass, node ast.Node, object types.Object) bool {
	if _, isDefer := node.(*ast.DeferStmt); isDefer {
		return false
	}
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if identifier, isIdentifier := inner.(*ast.Ident); isIdentifier && pass.TypesInfo.Uses[identifier] == object {
			found = true
		}
		return !found
	})
	return found
}

// WaitsIn are the calls of Wait or Run on named that node holds, outside function literals, which are judged on
// their own graphs. A deferred wait is not counted: it runs on the way out, after whatever the function reads first,
// and the rules do not follow paths to the function's exit.
func WaitsIn(pass *analysis.Pass, node ast.Node, named Command) []*ast.CallExpr {
	if _, isDefer := node.(*ast.DeferStmt); isDefer {
		return nil
	}
	found := []*ast.CallExpr{}
	ast.Inspect(node, func(inner ast.Node) bool {
		switch inner := inner.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			method, receiver := CommandMethod(pass, inner)
			if method != "Wait" && method != "Run" {
				return true
			}
			if waited, isNamed := CommandOf(pass.TypesInfo, receiver); isNamed && waited == named {
				found = append(found, inner)
			}
		}
		return true
	})
	return found
}

// CommandMethod is the name of the exec.Cmd method call calls and the expression it is called on, or "" and nil,
// resolved by the callee's package and receiver so a method of the same name on any other type is never mistaken
// for it. A method expression, (*exec.Cmd).Wait(cmd), is left alone: its command is an argument, not a receiver.
func CommandMethod(pass *analysis.Pass, call *ast.CallExpr) (string, ast.Expr) {
	callee := typeutil.StaticCallee(pass.TypesInfo, call)
	if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != "os/exec" {
		return "", nil
	}
	receiver := callee.Signature().Recv()
	if receiver == nil || !IsCommand(receiver.Type()) {
		return "", nil
	}
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return "", nil
	}
	selection, isSelection := pass.TypesInfo.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return "", nil
	}
	return callee.Name(), selector.X
}

// CommandOf names the command an expression reaches, through parentheses, & and *, or reports false when the
// expression is something no later line could name again, such as a call's result or an element of a slice. It names
// any variable the same way, so a rule can name a WaitGroup or a channel by it too.
func CommandOf(information *types.Info, expression ast.Expr) (Command, bool) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		object := information.ObjectOf(expression)
		if object == nil {
			return Command{}, false
		}
		return Command{Root: object}, true
	case *ast.SelectorExpr:
		selection, isSelection := information.Selections[expression]
		if !isSelection {
			// A qualified identifier: a package-level variable of another package.
			object := information.ObjectOf(expression.Sel)
			if object == nil {
				return Command{}, false
			}
			return Command{Root: object}, true
		}
		if selection.Kind() != types.FieldVal {
			return Command{}, false
		}
		inner, isNamed := CommandOf(information, expression.X)
		if !isNamed {
			return Command{}, false
		}
		return Command{Root: inner.Root, Path: inner.Path + "." + expression.Sel.Name}, true
	case *ast.StarExpr:
		return CommandOf(information, expression.X)
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return CommandOf(information, expression.X)
		}
	}
	return Command{}, false
}

// IsCommand says candidate is exec.Cmd or a pointer to it.
func IsCommand(candidate types.Type) bool {
	return IsNamed(candidate, "os/exec", "Cmd")
}

// IsNamed says candidate, or what it points to, is the named type name of the package at packagePath, by the package
// and the name, so a type of the same name declared anywhere else is never mistaken for it.
func IsNamed(candidate types.Type, packagePath string, name string) bool {
	dereferenced := candidate
	if pointer, isPointer := types.Unalias(candidate).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	named, isNamed := types.Unalias(dereferenced).(*types.Named)
	if !isNamed {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath && named.Obj().Name() == name
}

// Functions calls judge with each function's body and graph in the package inspected walks: every declaration with a
// body whose name type-checked, and every function literal. A function literal has a graph of its own, and the graph
// of the function around it does not descend into it, so each is judged on its own.
func Functions(
	pass *analysis.Pass,
	inspected *inspector.Inspector,
	graphs *ctrlflow.CFGs,
	judge func(body *ast.BlockStmt, graph *cfg.CFG),
) {
	inspected.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}, func(node ast.Node) {
		var graph *cfg.CFG
		var body *ast.BlockStmt
		switch function := node.(type) {
		case *ast.FuncDecl:
			// A declaration with no body has no graph, and one whose name did not type-check has none either.
			if function.Body == nil {
				return
			}
			if _, isFunction := pass.TypesInfo.Defs[function.Name].(*types.Func); !isFunction {
				return
			}
			graph = graphs.FuncDecl(function)
			body = function.Body
		case *ast.FuncLit:
			graph = graphs.FuncLit(function)
			body = function.Body
		}
		if graph != nil {
			judge(body, graph)
		}
	})
}
