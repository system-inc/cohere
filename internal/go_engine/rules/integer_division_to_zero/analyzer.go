// Package integer_division_to_zero is staticcheck's SA4025, a division of two integer literals whose result truncates
// to zero, such as 2 / 3, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4025/sa4025.go, whose logic it keeps and
// whose message it reports word for word, across the whole binary expression as the original's does. The original
// matches the pattern `(BinaryExpr (IntegerLiteral _) "/" (IntegerLiteral _))` through code.Matches; the port walks
// the binary expressions that pattern's entry node names and tests each by syntax and types, as the pattern's matcher
// does: both operands, their parentheses looked through, are integer literals by staticcheck_code.IntegerLiteral, the
// port of the helper that reads the pattern's IntegerLiteral. code.Matches skips a pattern only when the package holds
// none of the symbols it names, and this one names none, so every package is walked, generated files among them,
// since the original filters none. The message writes the division as staticcheck's report.Render prints it, called
// with a pass that holds only the file set, the one part of a pass Render reads, since Render is not ported yet. It
// reports through staticcheck_report.Report where the original reports through report.Report. The registry still runs
// the original, which stays the oracle: the parity tests beside this file run both over the same packages, and the two
// differ nowhere they hold.
package integer_division_to_zero

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/report"

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
			panic("integer_division_to_zero: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, expression)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4025",
	"SA4025",
	"Flags a division of two integer literals that truncates to zero, such as 2 / 3: staticcheck's SA4025, "+
		"ported as a check on syntax.",
	&Listener,
)

// Check judges one binary expression: a division whose operands are both written as integer literals, such as 2 / 3,
// -1 / 2 or (1) / 0x2, is a constant division of integers, which truncates, and one whose result is zero almost
// surely meant a fraction. It is zero wherever the result lands, a float64 constant or a float64 argument among them,
// since the literals divide as integers before the result is converted. A division with a named constant, a float
// literal or a conversion on either side is left alone, as the original leaves it, and so is one that truncates to
// anything but zero, such as 4 / 3.
func Check(context *rule.Context, expression *ast.BinaryExpr) {
	if expression.Op != token.QUO {
		return
	}
	if _, isLiteral := staticcheck_code.IntegerLiteral(context, expression.X); !isLiteral {
		return
	}
	if _, isLiteral := staticcheck_code.IntegerLiteral(context, expression.Y); !isLiteral {
		return
	}
	// The division's own recorded value, converted to an integer as the original converts it, since a result that
	// lands in a float type may be recorded as a float. A negative result, such as -3 / 2, has no unsigned value and
	// is not zero.
	quotient := constant.ToInt(context.TypesInfo.Types[expression].Value)
	if value, isUnsigned := constant.Uint64Val(quotient); !isUnsigned || value != 0 {
		return
	}
	rendered := report.Render(&analysis.Pass{Fset: context.Fset}, expression)
	staticcheck_report.Report(context, expression, fmt.Sprintf("the integer division '%s' results in zero", rendered))
}
