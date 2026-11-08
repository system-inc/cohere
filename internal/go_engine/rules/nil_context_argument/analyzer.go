// Package nil_context_argument is staticcheck's SA1012, a nil passed as a function's context.Context, which the
// context package forbids even where a function would allow it, ported as a check on syntax that reports through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1012/sa1012.go, whose logic it keeps and
// whose message and fixes it reports word for word, at the first argument's Pos and End, through
// staticcheck_report.Report, the port of the report.Report the original calls, proven against it beside it. The
// original matches (CallExpr fun@(Symbol _) (Builtin "nil"):_) over every call; the port asks the same by types and
// syntax, without the pattern package: the first argument, its parentheses looked through, is the universe's nil, as
// staticcheck_code.IsBuiltin answers, and the callee is a function as the pattern's Symbol reads it, the call's
// function, its parentheses and one instantiation looked through, an identifier or a selector whose object is a
// function, whose signature's first parameter is a context.Context. Its two suggested fixes put a call of the context
// package's placeholder context or of its background context in the nil's place, printed by staticcheck's
// analysis/edit, edit.Fix, edit.ReplaceWithNode and edit.Selector, borrowed in this wave, and passed through
// staticcheck_report.Fixes; wave 5 ports edit.
//
// It keeps the original's false positive: in a method expression's call, (*T).Method(nil, ctx), the nil is the
// receiver and not the first parameter, yet the signature read is the method's, whose first parameter is the context,
// so both report it; methodExpression in testdata/src/differences shows it. The original keeps no state from one call
// to the next. The registry runs the port in the original's place, under its name, and the original stays only as the
// oracle: the parity tests beside this file run both over the same packages, fixes included, and the two differ nowhere
// they hold.
package nil_context_argument

import (
	"go/ast"
	"go/types"

	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("nil_context_argument: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1012",
	"SA1012",
	"Flags a nil passed as a function's context.Context, where context.TODO or context.Background belongs: "+
		"staticcheck's SA1012, ported as a check on syntax.",
	&Listener,
)

// Check judges one call: a call of a function whose first parameter is a context.Context, given the universe's nil
// first, is reported at the nil, with the fixes that pass the context package's placeholder or background context
// instead.
func Check(context *rule.Context, call *ast.CallExpr) {
	if len(call.Args) == 0 || !staticcheck_code.IsBuiltin(context, call.Args[0], "nil") {
		return
	}
	function := symbolFunction(context, call.Fun)
	// A conversion, a builtin or a variable holding a function is no function the pattern binds.
	if function == nil {
		return
	}
	signature, isSignature := function.Type().(*types.Signature)
	if !isSignature {
		// Invariant: the type checker gives every function a signature.
		panic("nil_context_argument: a function's type is not a signature")
	}
	// A method expression of a method with no parameters takes the nil as its receiver.
	if signature.Params().Len() == 0 {
		return
	}
	if !typeutil.IsTypeWithName(signature.Params().At(0).Type(), "context.Context") {
		return
	}
	todo := &ast.CallExpr{Fun: edit.Selector("context", "TODO")}
	background := &ast.CallExpr{Fun: edit.Selector("context", "Background")}
	staticcheck_report.Report(context, call.Args[0],
		"do not pass a nil Context, even if a function permits it; pass context.TODO if you are unsure about which "+
			"Context to use",
		staticcheck_report.Fixes(
			edit.Fix("Use context.TODO", edit.ReplaceWithNode(context.Fset, call.Args[0], todo)),
			edit.Fix("Use context.Background", edit.ReplaceWithNode(context.Fset, call.Args[0], background))))
}

// symbolFunction is the function a call's function expression names as the pattern's Symbol reads it, and nil when it
// names none: the expression with its parentheses looked through, then one instantiation, IndexExpr or
// IndexListExpr, looked through to what it instantiates, parentheses again looked through, must be an identifier or a
// selector, and the object the type checker gives the identifier or the selected name must be a function.
func symbolFunction(context *rule.Context, function ast.Expr) *types.Func {
	named := ast.Unparen(function)
	switch instantiated := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := named.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(named)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(named.Sel)
	default:
		return nil
	}
	callee, isFunction := object.(*types.Func)
	if !isFunction {
		return nil
	}
	return callee
}
