// Package modulo_by_one is staticcheck's SA4028, a remainder of division by one, which is always zero, ported as a
// check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4028/sa4028.go, whose logic it keeps and
// whose message it reports word for word, across the whole binary expression as the original's does. The original
// matches the pattern `(BinaryExpr _ "%" (IntegerLiteral "1"))` through code.Matches; the port walks the binary
// expressions that pattern's entry node names and tests each by syntax and types, as the pattern's matcher does: the
// right operand, its parentheses looked through, is an integer literal by staticcheck_code.IntegerLiteral, the port of
// the helper that reads the pattern's IntegerLiteral, and its value prints as 1. code.Matches skips a pattern only when
// the package holds none of the symbols it names, and this one names none, so every package is walked, generated
// files among them, since the original filters none. It reports through staticcheck_report.Report where the original
// reports through report.Report. The registry still runs the original, which stays the oracle: the parity tests
// beside this file run both over the same packages, and the two differ nowhere they hold.
package modulo_by_one

import (
	"go/ast"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every binary expression, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.BinaryExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		expression, isBinary := node.(*ast.BinaryExpr)
		if !isBinary {
			// Invariant: the dispatcher hands Node only the binary expressions it was asked for.
			panic("modulo_by_one: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, expression)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4028",
	"SA4028",
	"Flags x % 1, which is always zero: staticcheck's SA4028, ported as a check on syntax.",
	&Listener,
)

// Check judges one binary expression: a remainder whose divisor is written as the integer literal 1, such as 1, 0x1,
// +1 or (1), is always zero. A divisor that is 1 only by a named constant, a conversion or arithmetic is left alone,
// as the original leaves it; the assignment x %= 1 is a statement, not a binary expression, and is left alone too.
func Check(context *rule.Context, expression *ast.BinaryExpr) {
	if expression.Op != token.REM {
		return
	}
	divisor, isLiteral := staticcheck_code.IntegerLiteral(context, expression.Y)
	// The pattern compares the literal's constant by how it prints, so this does too; a literal of integer kind has
	// one spelling of 1.
	if !isLiteral || divisor.Value.String() != "1" {
		return
	}
	staticcheck_report.Report(context, expression, "x % 1 is always zero")
}
