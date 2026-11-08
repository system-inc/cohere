// Package ineffective_url_query_modification is staticcheck's SA4027, a call of Add, Del or Set on the values
// (*net/url.URL).Query returns, which modifies a copy and leaves the URL as it was, ported as a check on syntax that
// reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4027/sa4027.go, whose message it reports
// word for word, over the whole call, through staticcheck_report.Report with no option, as the original reports
// through report.Report: a generated file is reported too. The original matches the pattern
// `(CallExpr (SelectorExpr (CallExpr (SelectorExpr recv (Ident "Query")) []) (Ident meth)) _)` through code.Matches;
// the port matches what that pattern matches without importing it: a call expression, any of them, since the pattern
// names no symbol for code.CouldMatchAny to rule a package out by, of any arguments, whose function is, through any
// parentheses, a selector whose operand is, through any parentheses, a call with no arguments of a selector named
// Query. The pattern has no field for a call's ellipsis, so neither does the port. The original then asks
// code.IsOfPointerToTypeWithName whether the receiver, its parentheses removed as the pattern's bindings remove them,
// is a pointer to net/url.URL, which is go/types/typeutil's IsPointerToTypeWithName of the receiver's type, and the
// port asks that directly, and whether the method is Add, Del or Set. The original keeps nothing from one call to the
// next. The registry runs the port in the original's place, under its name, and the original stays only as the oracle:
// the parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package ineffective_url_query_modification

import (
	"go/ast"
	"slices"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every call, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			// Invariant: the dispatcher hands Node only the calls it was asked for.
			panic("ineffective_url_query_modification: the dispatcher handed over a node that is not a call")
		}
		Check(context, call)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4027",
	"SA4027",
	"Flags a modification of the copy (*net/url.URL).Query returns: staticcheck's SA4027, ported as a check "+
		"on syntax.",
	&Listener,
)

// message is the original's, word for word.
const message = "(*net/url.URL).Query returns a copy, modifying it doesn't change the URL"

// modifyingMethods are the methods of url.Values the original names, each of which changes only the copy.
var modifyingMethods = []string{"Add", "Del", "Set"}

// Check judges one call: Add, Del or Set called on the result of calling Query with no arguments on a pointer to a
// net/url.URL, each named through any parentheses.
func Check(context *rule.Context, call *ast.CallExpr) {
	method, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector || !slices.Contains(modifyingMethods, method.Sel.Name) {
		return
	}
	query, isCall := ast.Unparen(method.X).(*ast.CallExpr)
	if !isCall || len(query.Args) != 0 {
		return
	}
	querySelector, isQuerySelector := ast.Unparen(query.Fun).(*ast.SelectorExpr)
	if !isQuerySelector || querySelector.Sel.Name != "Query" {
		return
	}
	// The pattern binds the receiver with its parentheses removed, and the original asks its type through them.
	if !typeutil.IsPointerToTypeWithName(context.TypesInfo.TypeOf(ast.Unparen(querySelector.X)), "net/url.URL") {
		return
	}
	staticcheck_report.Report(context, call, message)
}
