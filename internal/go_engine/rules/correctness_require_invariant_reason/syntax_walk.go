package correctness_require_invariant_reason

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// syntaxWalk is one walk of a file gathering what its panics are judged by: each panic statement with the function
// directly holding it, each variable a function declares holding what recover returned and whether anything writes it
// again, and whether any call is to a Must function. Functions are numbered in the order the walk meets them.
type syntaxWalk struct {
	context *rule.Context
	// panics is every statement calling the builtin panic, in the order the walk met them.
	panics []panicStatement
	// recovered is each variable a function declares holding what recover returned, `recovered := recover()` or `var
	// recovered = recover()`, nil until the walk meets one.
	recovered map[*types.Var]recoveredVariable
	// holdsMustCall is whether any call in the file is to a Must function.
	holdsMustCall bool
	// functions are the functions whose bodies hold the node the walk is on, innermost last, and functionsMet how
	// many the walk has met.
	functions    []openFunction
	functionsMet int
	// depth is how many nodes deep the walk is, the file itself one.
	depth int
}

// panicStatement is a statement calling the builtin panic, its call, and the number of the function directly holding
// it.
type panicStatement struct {
	statement ast.Node
	call      *ast.CallExpr
	function  int
}

// recoveredVariable is the number of the function declaring a variable that holds what recover returned, and whether
// anything writes the variable after declaring it, so it may hold something else when a panic reads it.
type recoveredVariable struct {
	function int
	written  bool
}

// openFunction is a function whose body holds the node the walk is on: its number, and the depth the walk met it at.
type openFunction struct {
	number int
	depth  int
}

// Visit gathers node, and closes a function as the walk leaves its node.
func (walk *syntaxWalk) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		if last := len(walk.functions) - 1; last >= 0 && walk.functions[last].depth == walk.depth {
			walk.functions = walk.functions[:last]
		}
		walk.depth--
		return walk
	}
	walk.depth++
	switch syntaxNode := node.(type) {
	case *ast.FuncDecl:
		// Each function is judged on its own, so a recover in one never excuses a panic in a closure it holds.
		if syntaxNode.Body != nil {
			walk.openFunction()
		}
	case *ast.FuncLit:
		walk.openFunction()
	case *ast.AssignStmt:
		for _, left := range syntaxNode.Lhs {
			walk.markWritten(left)
		}
		if syntaxNode.Tok == token.DEFINE {
			walk.declareDefined(syntaxNode)
		}
	case *ast.RangeStmt:
		if syntaxNode.Tok == token.ASSIGN {
			walk.markWritten(syntaxNode.Key)
			walk.markWritten(syntaxNode.Value)
		}
	case *ast.UnaryExpr:
		if syntaxNode.Op == token.AND {
			walk.markWritten(syntaxNode.X)
		}
	case *ast.ValueSpec:
		// Only one value per name pairs them; `var first, second = pair()` assigns from one call's results.
		if len(syntaxNode.Names) == len(syntaxNode.Values) {
			for index, name := range syntaxNode.Names {
				walk.declare(name, syntaxNode.Values[index])
			}
		}
	// panic returns no value, so a call to it is always a statement of its own, or the call a defer or go runs.
	case *ast.ExprStmt:
		call, _ := ast.Unparen(syntaxNode.X).(*ast.CallExpr)
		walk.gatherPanic(node, call)
	case *ast.DeferStmt:
		walk.gatherPanic(node, syntaxNode.Call)
	case *ast.GoStmt:
		walk.gatherPanic(node, syntaxNode.Call)
	case *ast.CallExpr:
		if !walk.holdsMustCall {
			_, walk.holdsMustCall = mustCallee(walk.context, syntaxNode)
		}
	}
	return walk
}

// openFunction opens the function whose node the walk has just met, numbering it.
func (walk *syntaxWalk) openFunction() {
	walk.functions = append(walk.functions, openFunction{number: walk.functionsMet, depth: walk.depth})
	walk.functionsMet++
}

// declareDefined declares what a define pairs, `recovered := recover()`.
func (walk *syntaxWalk) declareDefined(define *ast.AssignStmt) {
	// A define's left side is identifiers only; the compiler refuses anything else, and only one value per name pairs
	// them, since `first, second := pair()` assigns from one call's results.
	names := 0
	for _, left := range define.Lhs {
		if _, isIdentifier := left.(*ast.Ident); isIdentifier {
			names++
		}
	}
	if names != len(define.Rhs) {
		return
	}
	index := 0
	for _, left := range define.Lhs {
		if name, isIdentifier := left.(*ast.Ident); isIdentifier {
			walk.declare(name, define.Rhs[index])
			index++
		}
	}
}

// declare keeps name as recovered when it declares a variable in a function's body that value, a call to recover,
// gives its value. A package-level var is no function's, so no panic re-raises it.
func (walk *syntaxWalk) declare(name *ast.Ident, value ast.Expr) {
	if len(walk.functions) == 0 {
		return
	}
	variable, isVariable := walk.context.TypesInfo.Defs[name].(*types.Var)
	call, isCall := ast.Unparen(value).(*ast.CallExpr)
	if !isVariable || !isCall || !isBuiltin(walk.context, call, "recover") {
		return
	}
	if walk.recovered == nil {
		walk.recovered = map[*types.Var]recoveredVariable{}
	}
	walk.recovered[variable] = recoveredVariable{function: walk.functions[len(walk.functions)-1].number}
}

// markWritten marks a recovered variable written when expression names it: assigned, ranged into with `=`, or given
// away by its address. Only a recovered variable's writes decide anything, and a local variable is in scope only
// after its declaration, so the walk has always met the declaration before any write of it.
func (walk *syntaxWalk) markWritten(expression ast.Expr) {
	if walk.recovered == nil {
		return
	}
	// A declaration's own name is in Defs, never Uses, so only a later write is marked.
	name, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier {
		return
	}
	variable, isVariable := walk.context.TypesInfo.Uses[name].(*types.Var)
	if declared, isRecovered := walk.recovered[variable]; isVariable && isRecovered {
		declared.written = true
		walk.recovered[variable] = declared
	}
}

// gatherPanic keeps statement when call calls the builtin panic. A statement is always in some function's body.
func (walk *syntaxWalk) gatherPanic(statement ast.Node, call *ast.CallExpr) {
	if call == nil || len(walk.functions) == 0 || !isBuiltin(walk.context, call, "panic") {
		return
	}
	walk.panics = append(walk.panics, panicStatement{statement: statement, call: call,
		function: walk.functions[len(walk.functions)-1].number})
}

// isRecoveredValue is whether argument is what recover returned in function, the number of the function directly
// holding the panic: the call itself, or a variable function declares holding it that nothing writes again.
func (walk *syntaxWalk) isRecoveredValue(argument ast.Expr, function int) bool {
	switch value := ast.Unparen(argument).(type) {
	case *ast.CallExpr:
		return isBuiltin(walk.context, value, "recover")
	case *ast.Ident:
		variable, isVariable := walk.context.TypesInfo.Uses[value].(*types.Var)
		declared, isRecovered := walk.recovered[variable]
		return isVariable && isRecovered && declared.function == function && !declared.written
	}
	return false
}
