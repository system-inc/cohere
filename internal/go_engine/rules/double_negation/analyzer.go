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
// The registry still runs the original, which stays the oracle: the parity tests beside this file run both over the
// same packages, and the two differ nowhere they hold.
package double_negation

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name:     "SA4013",
	Doc:      "Flags a boolean negated twice, !!b, which is b: staticcheck's SA4013, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every unary expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("double_negation: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.UnaryExpr)(nil)}, func(node ast.Node) {
		expression, isUnary := node.(*ast.UnaryExpr)
		if !isUnary {
			// Invariant: Preorder hands this function only the unary expressions it was asked for.
			panic("double_negation: the inspector handed over a node that is not a unary expression")
		}
		Check(context, expression)
	})
	return nil, nil
}

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
			edit.Fix("Turn into single negation", edit.ReplaceWithNode(context.Fset, expression, single)),
			edit.Fix("Remove double negation", edit.ReplaceWithNode(context.Fset, expression, operand)),
		))
}
