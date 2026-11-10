// Package double_negation is staticcheck's SA4013, a boolean negated twice, !!b, which is b and either redundant or a
// typo, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4013/sa4013.go, whose logic it keeps and
// whose message and two fixes it reports word for word, across the whole outer negation as the original's does. The
// original matches the pattern `(UnaryExpr "!" single@(UnaryExpr "!" x))` through code.Matches; the port walks the
// unary expressions that pattern's entry node names and tests each by syntax, as the pattern's matcher does: a
// negation whose operand, its parentheses looked through, is a negation too. The bindings are what the matcher binds,
// each with its parentheses looked through, so in !(!(b)) single is !(b) and x is b. The fixes replace the outer
// negation with single and with x, printed by staticcheck's analysis/edit, borrowed in this wave as the original uses
// it, through staticcheck_report.Fixes. The pattern names no symbol, so code.Matches walks every package, generated
// files among them, since the original filters none.
//
// The original's second fix drops the parentheses x stood in, so !!(a || b) && c becomes a || b && c, which groups
// differently; the port keeps it, the original's, for parity, and the parity fixture's precedence function shows it.
// The registry runs the port in the original's place, under its name, and the original stays only as the oracle: the
// parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package double_negation

import (
	"go/ast"
	"go/token"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every unary expression, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.UnaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		expression, isUnary := node.(*ast.UnaryExpr)
		if !isUnary {
			// Invariant: the dispatcher hands Node only the unary expressions it was asked for.
			panic("double_negation: the dispatcher handed over a node that is not a unary expression")
		}
		Check(context, expression)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4013",
	"SA4013",
	"Flags a boolean negated twice, !!b, which is b: staticcheck's SA4013, ported as a check on syntax.",
	&Listener,
)

// Check judges one unary expression: a negation of a negation, !!b or !(!b), is b, and the fixes turn it into the
// single negation or remove both. In !!!b both the outer and the inner pair are judged, each a finding of its own.
func Check(context *rule.Context, expression *ast.UnaryExpr) {
	if expression.Op != token.NOT {
		return
	}
	single, isUnary := ast.Unparen(expression.X).(*ast.UnaryExpr)
	if !isUnary || single.Op != token.NOT {
		return
	}
	operand := ast.Unparen(single.X)
	staticcheck_report.Report(context, expression, "negating a boolean twice has no effect; is this a typo?",
		staticcheck_report.Fixes(
			edit.Fix("Turn into single negation", edit.ReplaceWithNode(context.FileSet, expression, single)),
			edit.Fix("Remove double negation", edit.ReplaceWithNode(context.FileSet, expression, operand)),
		))
}
