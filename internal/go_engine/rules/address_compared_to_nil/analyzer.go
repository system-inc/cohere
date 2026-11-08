// Package address_compared_to_nil is staticcheck's SA4022, the address of a variable compared with nil, which is never
// nil, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4022/sa4022.go, whose logic it keeps and
// whose message it reports word for word, across the whole binary expression as the original's does. The original
// matches the pattern `(BinaryExpr (UnaryExpr "&" _) (Or "==" "!=") (Builtin "nil"))` through code.Matches; the port
// walks the binary expressions that pattern's entry node names and tests each by syntax and types, as the pattern's
// matcher does: the left operand, its parentheses looked through, takes an address of anything, the operator is == or
// !=, and the right operand is the builtin nil by staticcheck_code.IsBuiltin, the pattern's Builtin as a helper.
// code.Matches skips a pattern only when the package holds none of the symbols it names, and a Builtin is no symbol,
// so every package is walked, generated files among them, since the original filters none. It reports through
// staticcheck_report.Report where the original reports through report.Report. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package address_compared_to_nil

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4022",
	Doc: "Flags the address of a variable compared with nil, which is never nil: staticcheck's SA4022, ported as a " +
		"check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every binary expression of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("address_compared_to_nil: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.BinaryExpr)(nil)}, func(node ast.Node) {
		expression, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: Preorder hands this function only the binary expressions it was asked for.
			panic("address_compared_to_nil: the inspector handed over a node that is not a binary expression")
		}
		Check(context, expression)
	})
	return nil, nil
}

// Check judges one binary expression: an address taken with &, of a variable, a field, an element or a composite
// literal, compared with nil by == or !=, is never nil, so the comparison's answer is known. Only that order is
// judged, as the original judges it: nil == &x is left alone, and so is a nil the code declares itself.
func Check(context *rule.Context, expression *ast.BinaryExpr) {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return
	}
	address, isUnary := ast.Unparen(expression.X).(*ast.UnaryExpr)
	if !isUnary || address.Op != token.AND {
		return
	}
	// A nil the code declares, in a block or at package scope, is not the builtin.
	if !staticcheck_code.IsBuiltin(context, expression.Y, "nil") {
		return
	}
	staticcheck_report.Report(context, expression, "the address of a variable cannot be nil")
}
