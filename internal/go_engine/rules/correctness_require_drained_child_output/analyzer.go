// Package correctness_require_drained_child_output is the Go engine's rule that a child's piped output is read
// before the code waits for the child to exit.
//
// exec.Cmd's StdoutPipe and StderrPipe hand back the read end of a pipe the child writes into. The pipe holds about
// 64 KB, and past that the child blocks on its next write until someone reads, so it never exits and Wait, or Run,
// which waits, never returns. os/exec's documentation calls a Wait before the reads have finished incorrect, and vet
// does not check it. Servers.app hung this way in Swift: a ps every three seconds, waited on before its output was
// read, parked a hung child each time its table outgrew the buffer. It is the Go twin of
// nexus/correctness-require-drained-child-output.
//
// Each function is judged on its own control-flow graph. A pipe is a local variable the function declares from
// StdoutPipe or StderrPipe and never writes again, of a command the function names and never reassigns. Any mention
// of the pipe after it is made, outside a defer statement, counts as handing it to something that reads it, a
// goroutine started before the wait included, since the rule cannot see how far a callee reads. Unknown falls to
// silence: a wait is reported only when some path from the pipe's creation reaches it with no mention on the way.
package correctness_require_drained_child_output

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-drained-child-output"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_drained_child_output",
	Doc: "Flags exec.Cmd's Wait or Run reached through the function's control-flow graph from a StdoutPipe or " +
		"StderrPipe of the same command with nothing reading the pipe on the way, since output past the pipe " +
		"buffer blocks the child and the wait never returns.",
	// ctrlflow builds each function's graph with calls that never return ending their path, so a wait after an
	// os.Exit or a log.Fatal is never reached.
	Requires: []*analysis.Analyzer{ctrlflow.Analyzer, inspect.Analyzer},
	Run:      run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var pipeUnreadAtWaitMessage = policy.MessageOf(Name, "pipeUnreadAtWait")

func run(pass *analysis.Pass) (any, error) {
	graphs, isGraphs := pass.ResultOf[ctrlflow.Analyzer].(*ctrlflow.CFGs)
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isGraphs || !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("correctness_require_drained_child_output: a required analyzer's result is missing")
	}
	// Each function is judged on its own graph. A function literal has a graph of its own, and the graph of the
	// function around it does not descend into it, so a pipe made in a literal is judged in the literal.
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
			judgeGraph(pass, body, graph)
		}
	})
	return nil, nil
}

// command names an exec.Cmd the way a function's code reaches it: the variable it starts from, and the fields walked
// from there, so cmd.StdoutPipe and cmd.Wait are the same command, and job.cmd.Wait another.
type command struct {
	root types.Object
	path string
}

// pipe is one pipe the rule follows: the local variable holding it, the stream it reads, the command whose output it
// is, and the graph node that makes it, at its index in its block.
type pipe struct {
	variable *types.Var
	stream   string
	command  command
	made     ast.Node
	block    *cfg.Block
	index    int
}

// judgeGraph reports, for every pipe a live block of graph makes, each wait on its command that some path from the
// pipe's creation reaches with nothing mentioning the pipe on the way. body is the function's body, read whole for
// the writes that make a pipe or a command one the rule cannot follow.
func judgeGraph(pass *analysis.Pass, body *ast.BlockStmt, graph *cfg.CFG) {
	for _, block := range graph.Blocks {
		if !block.Live {
			continue
		}
		for index, node := range block.Nodes {
			made, isPipe := pipeMade(pass, node)
			if !isPipe || rewritten(pass, body, made) {
				continue
			}
			made.block = block
			made.index = index
			for _, wait := range unreadWaits(pass, made) {
				method, _ := commandMethod(pass, wait)
				pass.Report(analysis.Diagnostic{
					Pos:      wait.Pos(),
					End:      wait.End(),
					Category: pipeUnreadAtWaitMessage.Id,
					Message:  pipeUnreadAtWaitMessage.Render(map[string]string{"stream": made.stream, "wait": method}),
				})
			}
		}
	}
}

// pipeMade reads node as the declaration of a pipe: `stdout, err := cmd.StdoutPipe()`, or the same with var, whose
// first name the statement declares, of a command a later line can name. A pipe assigned to `_` is dropped where it
// is made, and a variable assigned with = may hold something else on another path, so neither is followed.
func pipeMade(pass *analysis.Pass, node ast.Node) (pipe, bool) {
	var target *ast.Ident
	var value ast.Expr
	switch statement := node.(type) {
	case *ast.AssignStmt:
		if statement.Tok != token.DEFINE || len(statement.Lhs) == 0 || len(statement.Rhs) != 1 {
			return pipe{}, false
		}
		identifier, isIdentifier := statement.Lhs[0].(*ast.Ident)
		if !isIdentifier {
			return pipe{}, false
		}
		target = identifier
		value = statement.Rhs[0]
	case *ast.ValueSpec:
		if len(statement.Names) == 0 || len(statement.Values) != 1 {
			return pipe{}, false
		}
		target = statement.Names[0]
		value = statement.Values[0]
	default:
		return pipe{}, false
	}
	if target.Name == "_" {
		return pipe{}, false
	}
	// A := that redeclares a variable already in scope defines nothing for it, so Defs holds no variable.
	variable, isVariable := pass.TypesInfo.Defs[target].(*types.Var)
	if !isVariable {
		return pipe{}, false
	}
	call, isCall := ast.Unparen(value).(*ast.CallExpr)
	if !isCall {
		return pipe{}, false
	}
	method, receiver := commandMethod(pass, call)
	var stream string
	switch method {
	case "StdoutPipe":
		stream = "stdout"
	case "StderrPipe":
		stream = "stderr"
	default:
		return pipe{}, false
	}
	named, isNamed := commandOf(pass.TypesInfo, receiver)
	if !isNamed {
		return pipe{}, false
	}
	return pipe{variable: variable, stream: stream, command: named, made: node}, true
}

// rewritten says body writes the pipe's variable anywhere besides its declaration, takes its address, or assigns the
// command's root variable or a field on the way to the command, so the variable may hold another reader or the
// command another child on some path. A write inside a function literal counts as much as one in the body, since
// the rule cannot place when the literal runs.
func rewritten(pass *analysis.Pass, body *ast.BlockStmt, made pipe) bool {
	found := false
	writes := func(target ast.Expr) {
		// A name this statement declares is a new variable, never a write to one the rule follows.
		if identifier, isIdentifier := ast.Unparen(target).(*ast.Ident); isIdentifier &&
			pass.TypesInfo.Defs[identifier] != nil {
			return
		}
		named, isNamed := commandOf(pass.TypesInfo, target)
		if !isNamed {
			return
		}
		if named.root == made.variable {
			found = true
		}
		if named.root == made.command.root &&
			(named.path == made.command.path || strings.HasPrefix(made.command.path, named.path+".")) {
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
				pass.TypesInfo.Uses[identifier] == made.variable {
				found = true
			}
		}
		return !found
	})
	return found
}

// unreadWaits are the waits on the pipe's command that a path from just after the pipe's creation reaches with no
// node on the way mentioning the pipe, each once, in the order the walk meets them. A path ends at a node that
// mentions the pipe, at a wait, and at the creation itself, which a loop that leads back to it makes a new pipe that
// is judged on its own. The creation's block is entered mid-way first, and from its top if a loop returns to it.
func unreadWaits(pass *analysis.Pass, made pipe) []*ast.CallExpr {
	type step struct {
		block *cfg.Block
		from  int
	}
	waits := []*ast.CallExpr{}
	reported := map[*ast.CallExpr]bool{}
	visited := map[*cfg.Block]bool{}
	pending := []step{{block: made.block, from: made.index + 1}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		ended := false
		for _, node := range current.block.Nodes[current.from:] {
			if node == made.made || mentions(pass, node, made.variable) {
				ended = true
				break
			}
			found := waitsIn(pass, node, made.command)
			for _, wait := range found {
				if !reported[wait] {
					reported[wait] = true
					waits = append(waits, wait)
				}
			}
			if len(found) > 0 {
				ended = true
				break
			}
		}
		if ended {
			continue
		}
		for _, successor := range current.block.Succs {
			// A block no path from the entry reaches never runs.
			if !successor.Live || visited[successor] {
				continue
			}
			visited[successor] = true
			pending = append(pending, step{block: successor, from: 0})
		}
	}
	return waits
}

// mentions says node names the pipe's variable anywhere in it, function literals included, so a goroutine that copies
// the pipe counts as a drain from where it is started. A defer statement never counts: its call runs on the way out,
// after the wait.
func mentions(pass *analysis.Pass, node ast.Node, variable *types.Var) bool {
	if _, isDefer := node.(*ast.DeferStmt); isDefer {
		return false
	}
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if identifier, isIdentifier := inner.(*ast.Ident); isIdentifier && pass.TypesInfo.Uses[identifier] == variable {
			found = true
		}
		return !found
	})
	return found
}

// waitsIn are the calls of Wait or Run on named that node holds, outside function literals, which are judged on
// their own graphs. A deferred wait is not counted: it runs on the way out, after whatever the function reads first,
// and the rule does not follow paths to the function's exit.
func waitsIn(pass *analysis.Pass, node ast.Node, named command) []*ast.CallExpr {
	if _, isDefer := node.(*ast.DeferStmt); isDefer {
		return nil
	}
	found := []*ast.CallExpr{}
	ast.Inspect(node, func(inner ast.Node) bool {
		switch inner := inner.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			method, receiver := commandMethod(pass, inner)
			if method != "Wait" && method != "Run" {
				return true
			}
			if waited, isNamed := commandOf(pass.TypesInfo, receiver); isNamed && waited == named {
				found = append(found, inner)
			}
		}
		return true
	})
	return found
}

// commandMethod is the name of the exec.Cmd method call calls and the expression it is called on, or "" and nil,
// resolved by the callee's package and receiver so a method of the same name on any other type is never mistaken
// for it. A method expression, (*exec.Cmd).Wait(cmd), is left alone: its command is an argument, not a receiver.
func commandMethod(pass *analysis.Pass, call *ast.CallExpr) (string, ast.Expr) {
	callee := typeutil.StaticCallee(pass.TypesInfo, call)
	if callee == nil || callee.Pkg() == nil || callee.Pkg().Path() != "os/exec" {
		return "", nil
	}
	receiver := callee.Signature().Recv()
	if receiver == nil || !isCommand(receiver.Type()) {
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

// commandOf names the command an expression reaches, through parentheses, & and *, or reports false when the
// expression is something no later line could name again, such as a call's result or an element of a slice.
func commandOf(information *types.Info, expression ast.Expr) (command, bool) {
	switch expression := ast.Unparen(expression).(type) {
	case *ast.Ident:
		object := information.ObjectOf(expression)
		if object == nil {
			return command{}, false
		}
		return command{root: object}, true
	case *ast.SelectorExpr:
		selection, isSelection := information.Selections[expression]
		if !isSelection {
			// A qualified identifier: a package-level variable of another package.
			object := information.ObjectOf(expression.Sel)
			if object == nil {
				return command{}, false
			}
			return command{root: object}, true
		}
		if selection.Kind() != types.FieldVal {
			return command{}, false
		}
		inner, isNamed := commandOf(information, expression.X)
		if !isNamed {
			return command{}, false
		}
		return command{root: inner.root, path: inner.path + "." + expression.Sel.Name}, true
	case *ast.StarExpr:
		return commandOf(information, expression.X)
	case *ast.UnaryExpr:
		if expression.Op == token.AND {
			return commandOf(information, expression.X)
		}
	}
	return command{}, false
}

// isCommand says candidate is exec.Cmd or a pointer to it.
func isCommand(candidate types.Type) bool {
	dereferenced := candidate
	if pointer, isPointer := types.Unalias(candidate).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	named, isNamed := types.Unalias(dereferenced).(*types.Named)
	if !isNamed {
		return false
	}
	return named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "os/exec" && named.Obj().Name() == "Cmd"
}
