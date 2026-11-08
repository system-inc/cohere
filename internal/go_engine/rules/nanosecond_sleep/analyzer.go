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
// edit.ReplaceWithPattern drops it, and format.Node fails only on syntax it cannot print. The registry runs the port in
// the original's place, under its name, and the original stays only as the oracle: the parity tests beside this file
// run both over the same packages, and the two differ nowhere they hold.
package nanosecond_sleep

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/edit"

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
			panic("nanosecond_sleep: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1004",
	"SA1004",
	"Flags time.Sleep given an integer literal from 1 to 120, nanoseconds most likely meant as seconds: "+
		"staticcheck's SA1004, ported.",
	&Listener,
)

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
