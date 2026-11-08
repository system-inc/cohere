// Package nanosecond_sleep is staticcheck's SA1004, time.Sleep given a small integer literal, which sleeps that many
// nanoseconds where seconds were most likely meant, ported as a check on types and syntax that reports through the
// kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1004/sa1004.go, whose logic it keeps and
// whose message and two fixes it reports word for word, at the same range, the literal. The original matches its
// query, `(CallExpr (Symbol "time.Sleep") lit@(IntegerLiteral value))`, through staticcheck's pattern language, and
// builds each fix from one more, `(BinaryExpr duration "*" (SelectorExpr (Ident "time") (Ident "Nanosecond")))` and
// its Second twin, through edit.ReplaceWithPattern. The port asks the same of each call directly, reading
// pattern/match.go for what each construct of the query matches and asking staticcheck_code.IntegerLiteral where the
// query names IntegerLiteral, builds each fix's syntax itself as pattern.NodeToAST builds it, around the literal's own
// node, and prints it with staticcheck's analysis/edit, borrowed in this wave as the original uses it, through
// staticcheck_report.Fixes. One difference no fixture can reach: edit.ReplaceWithNode panics on a format error where
// edit.ReplaceWithPattern drops it, and format.Node fails only on syntax it cannot print. The registry still runs the
// original, which stays the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package nanosecond_sleep

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA1004",
	Doc: "Flags time.Sleep given an integer literal from 1 to 120, nanoseconds most likely meant as seconds: " +
		"staticcheck's SA1004, ported.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every call of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("nanosecond_sleep: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(node ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: Preorder hands this function only the calls it was asked for.
			panic("nanosecond_sleep: the inspector handed over a node that is not a call")
		}
		Check(context, call)
	})
	return nil, nil
}

// Check judges one call: a call of time.Sleep whose one argument is an integer literal of a value from 1 to 120,
// negative values included, sleeps nanoseconds where seconds were most likely meant, and the fixes multiply the
// literal by time.Nanosecond, saying so, or by time.Second.
func Check(context *rule.Context, call *ast.CallExpr) {
	if !isTimeSleep(context, call.Fun) {
		return
	}
	// The query's argument is one node, which matches an argument list of exactly one. time.Sleep takes one, so a call
	// that type-checks always has it.
	if len(call.Args) != 1 {
		return
	}
	typeAndValue, isLiteral := staticcheck_code.IntegerLiteral(context, call.Args[0])
	if !isLiteral {
		return
	}
	nanoseconds, isExact := constant.Int64Val(typeAndValue.Value)
	if !isExact {
		return
	}
	// time.Sleep(0) is a seldom used pattern in concurrency tests. Above 120 might be intentional; 120 is where the
	// user could have meant two minutes.
	if nanoseconds == 0 || nanoseconds > 120 {
		return
	}

	// `lit` binds the argument as the matcher reached it, through its outer parentheses, so the finding and the fixes
	// cover the literal inside them.
	literal := ast.Unparen(call.Args[0])
	staticcheck_report.Report(context, literal,
		fmt.Sprintf("sleeping for %d nanoseconds is probably a bug; be explicit if it isn't", nanoseconds),
		staticcheck_report.Fixes(
			edit.Fix("Explicitly use nanoseconds",
				edit.ReplaceWithNode(context.Fset, literal, multipliedBy(literal, "Nanosecond"))),
			edit.Fix("Use seconds", edit.ReplaceWithNode(context.Fset, literal, multipliedBy(literal, "Second"))),
		))
}

// multipliedBy is literal times the time package's unit, as pattern.NodeToAST builds the fix's pattern: the
// literal's own node, not a copy, so format prints it from its own positions, and the package spelled time as
// written, even where the file imports it under another name or into its own scope, as the original's fix does.
func multipliedBy(literal ast.Expr, unit string) ast.Expr {
	return &ast.BinaryExpr{X: literal, Op: token.MUL, Y: edit.Selector("time", unit)}
}

// isTimeSleep is whether function names time.Sleep, as the query's `(Symbol "time.Sleep")` matches it: an
// identifier or a selector, parenthesized or not and instantiated or not, whose object is the function time.Sleep,
// so a dot import and a renamed import name it, and a method, a variable or a local function named Sleep does not.
// Symbol also names a package's variable, constant or type by its package's path and name, and none spelled
// time.Sleep exists, since only the standard library's time has that path and it declares Sleep as a function alone.
func isTimeSleep(context *rule.Context, function ast.Expr) bool {
	// The name is the function without its parentheses or its instantiation, itself parenthesized or not.
	name := ast.Unparen(function)
	switch instantiated := name.(type) {
	case *ast.IndexExpr:
		name = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		name = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch name := name.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(name)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(name.Sel)
	default:
		return false
	}
	// FullName spells a method with its receiver, `(T).Sleep`, and a function by its package's path.
	sleep, isFunction := object.(*types.Func)
	return isFunction && sleep.FullName() == "time.Sleep"
}
