// Package case_insensitive_comparison is staticcheck's SA6005, two strings compared for equality after both are
// lowered or both raised with the strings package, which strings.EqualFold does without allocating either copy,
// ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa6005/sa6005.go, whose logic it keeps and
// whose message and fix it reports word for word, over the same range, the comparison. The original matches the
// pattern `(BinaryExpr (CallExpr fun@(Symbol (Or "strings.ToLower" "strings.ToUpper")) [a]) tok@(Or "==" "!=")
// (CallExpr fun [b]))` of staticcheck's pattern package and builds its fix from `(CallExpr (SelectorExpr (Ident
// "strings") (Ident "EqualFold")) [a b])`; the port tests the same shape by its syntax: an == or != between two
// calls of one argument each, parentheses around either call looked through as the pattern does, the left one's
// function strings.ToLower or strings.ToUpper as staticcheck_code.SymbolIndex judges the two, and the right one's the
// very same function object, named by an identifier or a selector, which is how the pattern's binding fun is recalled
// (calledFunction). It builds the fix's syntax itself as pattern.NodeToAST builds it, around the two arguments with
// their parentheses looked through as the bindings a and b hold them, and prints it with staticcheck's analysis/edit,
// borrowed in this wave as the original uses it, through staticcheck_report.Fixes; like the original's, the fix
// names the package strings whatever name the file imports it under. The original first asks whether the package
// depends on the strings package at all, code.CouldMatchAny; any comparison the pattern matches calls into it, so the
// question changes no finding, and the port, asking nothing, walks every binary expression. It reports with no
// option, so a generated file is judged like any other. The registry runs the port in the original's place, under its
// name, and the original stays only as the oracle: the parity tests beside this file run both over the same packages,
// and the two differ nowhere they hold.
package case_insensitive_comparison

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.BinaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		comparison, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: the dispatcher hands Node only the binary expressions it was asked for.
			panic("case_insensitive_comparison: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, comparison)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA6005",
	"SA6005",
	"Flags two strings compared after strings.ToLower or strings.ToUpper, where strings.EqualFold is meant: "+
		"staticcheck's SA6005, ported as a check on syntax.",
	&Listener,
)

// Check judges one binary expression: an equality or inequality of two calls of one function, lowering or raising
// one argument each.
func Check(context *rule.Context, comparison *ast.BinaryExpr) {
	if comparison.Op != token.EQL && comparison.Op != token.NEQ {
		return
	}
	left, isLeftCall := ast.Unparen(comparison.X).(*ast.CallExpr)
	right, isRightCall := ast.Unparen(comparison.Y).(*ast.CallExpr)
	if !isLeftCall || !isRightCall || len(left.Args) != 1 || len(right.Args) != 1 {
		return
	}
	if staticcheck_code.SymbolIndex(context, left.Fun, "strings.ToLower", "strings.ToUpper") < 0 {
		return
	}
	function := calledFunction(context, left.Fun)
	if function == nil || calledFunction(context, right.Fun) != function {
		return
	}
	method := "strings.EqualFold"
	var replacement ast.Expr = &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: &ast.Ident{Name: "strings"}, Sel: &ast.Ident{Name: "EqualFold"}},
		Args: []ast.Expr{ast.Unparen(left.Args[0]), ast.Unparen(right.Args[0])},
	}
	if comparison.Op == token.NEQ {
		replacement = &ast.UnaryExpr{Op: token.NOT, X: replacement}
		method = "!" + method
	}
	staticcheck_report.Report(context, comparison, fmt.Sprintf("should use %s instead", method),
		staticcheck_report.Fixes(
			edit.Fix("replace with "+method, edit.ReplaceWithNode(context.Fset, comparison, replacement)),
		))
}

// calledFunction is the object a call's function names, an identifier or a selector, its parentheses looked
// through, or nil when it is neither. The pattern recalls its binding fun, the object the left call's function names,
// against the right call's function exactly this way.
func calledFunction(context *rule.Context, function ast.Expr) types.Object {
	switch named := ast.Unparen(function).(type) {
	case *ast.Ident:
		return context.TypesInfo.ObjectOf(named)
	case *ast.SelectorExpr:
		return context.TypesInfo.ObjectOf(named.Sel)
	}
	return nil
}
