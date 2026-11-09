// Package negative_zero_float is staticcheck's SA4026, a negative zero written as a constant, -0.0 or -float64(0),
// which Go folds to positive zero, ported as a check on syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4026/sa4026.go, whose logic it keeps and
// whose messages and fix it reports word for word, across the whole expression the pattern matched as the original's
// do. The original matches the pattern
//
//	(Or
//		(UnaryExpr "-" (BasicLit "FLOAT" "0.0"))
//		(UnaryExpr "-" (CallExpr conv@(Object (Or "float32" "float64"))
//			lit@(Or (BasicLit "INT" "0") (BasicLit "FLOAT" "0.0"))))
//		(CallExpr conv@(Object (Or "float32" "float64")) (UnaryExpr "-" lit@(BasicLit "INT" "0"))))
//
// through code.Matches; the port walks the unary expressions and calls that pattern's entry nodes name and tests each
// by syntax, as the pattern's matcher does, every operand, argument and function with its parentheses looked through.
// A BasicLit compares the literal's text, so 0.0 is not 0., 0.00 or 0e0, and 0 is not 00 or 0x0. An Object is any
// identifier of its name, whatever it resolves to, with no universe check as a Builtin has: a float64 the code
// declares as a type is a conversion, and the message names it, and one it declares as a variable binds an object
// that is no type name, for which the original reports its -0.0 message, as the port does. The pattern names no
// symbol, so code.Matches walks every package, generated files among them, since the original filters none. The
// messages render code through staticcheck_report.Render, and the fix is printed by staticcheck's analysis/edit,
// borrowed in this wave as the original uses it, through staticcheck_report.Fixes. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and the two differ nowhere they hold.
package negative_zero_float

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/analysis/edit"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every unary expression and call, in preorder.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.UnaryExpr)(nil), (*ast.CallExpr)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		switch expression := node.(type) {
		case *ast.UnaryExpr:
			CheckNegation(context, expression)
		case *ast.CallExpr:
			CheckConversion(context, expression)
		default:
			// Invariant: the dispatcher hands Node only the unary expressions and calls it was asked for.
			panic("negative_zero_float: the dispatcher handed over a node that is neither a unary expression nor " +
				"a call")
		}
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4026",
	"SA4026",
	"Flags a negative zero written as a constant, -0.0 or -float64(0), which Go folds to positive zero: "+
		"staticcheck's SA4026, ported as a check on syntax.",
	&Listener,
)

// conversionNames are the names the pattern's Object accepts for the conversion.
var conversionNames = []string{"float32", "float64"}

// fixMessage is what the one fix says, whichever expression it replaces.
const fixMessage = "Use math.Copysign to create negative zero"

// CheckNegation judges one unary expression: -0.0 is positive zero, and so is -float32(0), -float64(0) or either
// converting 0.0.
func CheckNegation(context *rule.Context, negation *ast.UnaryExpr) {
	if negation.Op != token.SUB {
		return
	}
	operand := ast.Unparen(negation.X)
	if isLiteral(operand, token.FLOAT, "0.0") {
		reportLiteral(context, negation)
		return
	}
	call, isCall := operand.(*ast.CallExpr)
	if !isCall {
		return
	}
	conversion := conversionName(call.Fun)
	// The pattern's lit is one node, which matches an argument list of exactly one.
	if conversion == nil || len(call.Args) != 1 {
		return
	}
	literal := ast.Unparen(call.Args[0])
	if !isLiteral(literal, token.INT, "0") && !isLiteral(literal, token.FLOAT, "0.0") {
		return
	}
	reportConversion(context, negation, conversion, literal)
}

// CheckConversion judges one call: float32(-0) and float64(-0) are positive zero. A -0.0 converted is the negation's
// to judge, so the call is left alone.
func CheckConversion(context *rule.Context, call *ast.CallExpr) {
	conversion := conversionName(call.Fun)
	if conversion == nil || len(call.Args) != 1 {
		return
	}
	negation, isNegation := ast.Unparen(call.Args[0]).(*ast.UnaryExpr)
	if !isNegation || negation.Op != token.SUB {
		return
	}
	literal := ast.Unparen(negation.X)
	if !isLiteral(literal, token.INT, "0") {
		return
	}
	reportConversion(context, call, conversion, literal)
}

// conversionName is the identifier function is, its parentheses looked through, when it is spelled float32 or
// float64, and nil otherwise. It asks nothing of what the identifier resolves to, as the pattern's Object does not.
func conversionName(function ast.Expr) *ast.Ident {
	identifier, isIdentifier := ast.Unparen(function).(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	for _, name := range conversionNames {
		if identifier.Name == name {
			return identifier
		}
	}
	return nil
}

// isLiteral is whether expression is a basic literal of kind written exactly as text, as the pattern's BasicLit
// compares a literal's kind and its text.
func isLiteral(expression ast.Expr, kind token.Token, text string) bool {
	literal, isBasic := expression.(*ast.BasicLit)
	return isBasic && literal.Kind == kind && literal.Value == text
}

// reportLiteral reports node as a -0.0, with the fix that writes math.Copysign in its place.
func reportLiteral(context *rule.Context, node ast.Expr) {
	staticcheck_report.Report(context, node,
		"in Go, the floating-point literal '-0.0' is the same as '0.0', it does not produce a negative zero",
		staticcheck_report.Fixes(edit.Fix(fixMessage, edit.ReplaceWithString(node, `math.Copysign(0, -1)`))))
}

// reportConversion reports node, a conversion of a zero literal negated on either side, naming the conversion and
// the literal, with the fix that writes math.Copysign in its place, converted to float32 where the conversion is
// float32. A conversion whose name resolves to no type, a variable or a function the code declares, is reported as a
// -0.0, as the original reports it.
func reportConversion(context *rule.Context, node ast.Expr, conversion *ast.Ident, literal ast.Expr) {
	typeName, isTypeName := context.TypesInfo.ObjectOf(conversion).(*types.TypeName)
	if !isTypeName {
		reportLiteral(context, node)
		return
	}
	replacement := `math.Copysign(0, -1)`
	if typeName.Name() == "float32" {
		replacement = `float32(math.Copysign(0, -1))`
	}
	staticcheck_report.Report(context, node,
		fmt.Sprintf("in Go, the floating-point expression '%s' is the same as '%s(%s)', it does not produce a "+
			"negative zero", staticcheck_report.Render(context, node), typeName.Name(),
			staticcheck_report.Render(context, literal)),
		staticcheck_report.Fixes(edit.Fix(fixMessage, edit.ReplaceWithString(node, replacement))))
}
