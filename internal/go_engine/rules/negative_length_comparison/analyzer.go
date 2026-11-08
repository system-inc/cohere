// Package negative_length_comparison is staticcheck's SA4024, a test of whether len or cap is below zero, which they
// never are, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4024/sa4024.go, whose logic it keeps and
// whose message it reports word for word, across the whole binary expression as the original's does. The original
// matches the pattern `(Or (BinaryExpr (IntegerLiteral "0") ">" (CallExpr (Builtin (Or "len" "cap")) _))
// (BinaryExpr (CallExpr (Builtin (Or "len" "cap")) _) "<" (IntegerLiteral "0")))` through code.Matches; the port walks
// the binary expressions that pattern's entry nodes name and tests each by syntax and types, as the pattern's matcher
// does: 0 > a call or a call < 0, each operand with its parentheses looked through, the zero an integer literal by
// staticcheck_code.IntegerLiteral that prints as 0, and the call's function, its parentheses looked through too, len
// or cap as staticcheck_code.IsBuiltin judges the pattern's Builtin. code.Matches skips a pattern only when the package
// holds none of the symbols it names, and a Builtin is no symbol, so every package is walked, generated files among
// them, since the original filters none. It reports through staticcheck_report.Report where the original reports
// through report.Report. The registry still runs the original, which stays the oracle: the parity tests beside this
// file run both over the same packages, and the two differ nowhere they hold.
package negative_length_comparison

import (
	"fmt"
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
			panic("negative_length_comparison: the dispatcher handed over a node that is not a binary expression")
		}
		Check(context, expression)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4024",
	"SA4024",
	"Flags len or cap compared below zero, which they never return: staticcheck's SA4024, ported as a check on "+
		"syntax.",
	&Listener,
)

// builtinNames are the builtins the pattern's Or names, in its order.
var builtinNames = []string{"len", "cap"}

// Check judges one binary expression: 0 > len(x), 0 > cap(x), len(x) < 0 and cap(x) < 0 are always false, since
// neither builtin returns a negative value. Only those two orders are judged, as the original judges them, so
// len(x) <= 0, 0 < len(x) and a zero that is a named constant or a float literal are left alone, and so is a len or
// cap the code declares itself.
func Check(context *rule.Context, expression *ast.BinaryExpr) {
	var zero ast.Expr
	var call ast.Expr
	switch expression.Op {
	case token.GTR:
		zero, call = expression.X, expression.Y
	case token.LSS:
		call, zero = expression.X, expression.Y
	default:
		return
	}
	builtin := builtinCalled(context, call)
	if builtin == "" {
		return
	}
	literal, isLiteral := staticcheck_code.IntegerLiteral(context, zero)
	// The pattern compares the literal's constant by how it prints, as -0 and 0x0 both do.
	if !isLiteral || literal.Value.String() != "0" {
		return
	}
	staticcheck_report.Report(context, expression,
		fmt.Sprintf("builtin function %s does not return negative values", builtin))
}

// builtinCalled is the name of the builtin, len or cap, that expression, its parentheses looked through, calls with
// any arguments, its function's parentheses looked through too, and empty for any other expression.
func builtinCalled(context *rule.Context, expression ast.Expr) string {
	call, isCall := ast.Unparen(expression).(*ast.CallExpr)
	if !isCall {
		return ""
	}
	for _, name := range builtinNames {
		if staticcheck_code.IsBuiltin(context, call.Fun, name) {
			return name
		}
	}
	return ""
}
