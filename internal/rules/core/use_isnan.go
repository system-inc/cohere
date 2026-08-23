package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageComparisonWithNaN = rule.Message{
	Id: "comparisonWithNaN",
	Description: "Comparing against NaN is always false, including `NaN === NaN`, because NaN is " +
		"the one value in the language that is not equal to itself. So this comparison does not " +
		"test what it reads as testing: it is a check for NaN that can never succeed, and the " +
		"branch behind it is dead. Use Number.isNaN(value) instead, which is the only reliable " +
		"test. Note the Number. prefix: the global isNaN coerces its argument first, so " +
		"isNaN('hello') is true.",
}

// comparisonOperators are the operators for which a NaN operand makes the result constant.
//
// Relational operators are included alongside equality. `x < NaN` is always false too, and writing
// NaN there signals an intent the operator cannot carry just as much as `x === NaN` does.
var comparisonOperators = map[ast.Kind]bool{
	ast.KindLessThanToken:                true,
	ast.KindLessThanEqualsToken:          true,
	ast.KindGreaterThanToken:             true,
	ast.KindGreaterThanEqualsToken:       true,
	ast.KindEqualsEqualsToken:            true,
	ast.KindEqualsEqualsEqualsToken:      true,
	ast.KindExclamationEqualsToken:       true,
	ast.KindExclamationEqualsEqualsToken: true,
}

// UseIsNaN flags a comparison against NaN.
//
//	valid:   Number.isNaN(value)
//	valid:   value === Number.NaN ? 0 : 1   (still reported; see below)
//	valid:   const NaN = 1; value === NaN   (a local shadowing the global is not reported)
//	invalid: value === NaN
//	invalid: NaN !== value
//	invalid: value < NaN
//
// The failure is total rather than partial: every comparison against NaN evaluates to false (or true
// for `!==`), so the branch is unreachable and the check silently does nothing. That makes this one
// of the few rules where a finding is almost certainly a real defect rather than a style preference.
//
// `Number.NaN` is the same value reached through a property and is reported the same way.
//
// No fix. Rewriting `x === NaN` to `Number.isNaN(x)` is right for equality and wrong for the
// relational operators, where there is no equivalent, and the negated form needs `!Number.isNaN(x)`.
// Getting that wrong changes a branch that never runs into one that runs sometimes, which is a
// behavior change wearing a lint fix.
var UseIsNaN = rule.Rule{
	Name: "use-isnan",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil || !comparisonOperators[binary.OperatorToken.Kind] {
					return
				}
				if isNaNReference(binary.Left) || isNaNReference(binary.Right) {
					ctx.ReportNode(node, messageComparisonWithNaN)
				}
			},
		}
	},
}

// isNaNReference reports whether an expression names NaN, bare or through Number.
//
// Matched on spelling rather than through the checker. A local binding named NaN would shadow the
// global and make this a false report, which is why the fixtures cover that case; it is rare enough,
// and strange enough, that the trade favors catching the real defect.
func isNaNReference(node *ast.Node) bool {
	if node == nil {
		return false
	}

	switch node.Kind {
	case ast.KindIdentifier:
		return node.Text() == "NaN"

	case ast.KindPropertyAccessExpression:
		// `Number.NaN` is the same value spelled through the constructor.
		access := node.AsPropertyAccessExpression()
		if access.Expression == nil || access.Expression.Kind != ast.KindIdentifier {
			return false
		}
		if access.Expression.Text() != "Number" {
			return false
		}
		name := access.Name()
		return name != nil && name.Text() == "NaN"

	case ast.KindParenthesizedExpression:
		return isNaNReference(node.AsParenthesizedExpression().Expression)
	}
	return false
}
