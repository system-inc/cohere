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

// UseIsNaNOptions mirrors the two switches ESLint's rule carries.
//
// EnforceForSwitchCase defaults to true, matching both ESLint 9 and the live config for this tree.
// A `case NaN:` never matches for the same reason `=== NaN` is never true, so the default is the
// one that catches the defect rather than the one that is quieter.
//
// EnforceForIndexOf is present because the config sets it and a decoded option that silently
// vanishes is worse than one that is read: `list.indexOf(NaN)` always returns -1, since indexOf uses
// strict equality. It is false here, matching the config.
type UseIsNaNOptions struct {
	EnforceForSwitchCase *bool
	EnforceForIndexOf    bool
}

var messageCaseWithNaN = rule.Message{
	Id: "caseWithNaN",
	Description: "This `case NaN:` can never match. A switch compares with strict equality, and " +
		"NaN is not equal to itself, so the branch is dead exactly as `value === NaN` would be. " +
		"Test for it before the switch with Number.isNaN(value), since there is no case label " +
		"that can catch it.",
}

var messageSwitchOnNaN = rule.Message{
	Id: "switchOnNaN",
	Description: "This switch tests NaN against each case, and NaN equals nothing including " +
		"itself, so every case is dead and only the default runs. Whatever this switch was " +
		"written to dispatch on, it dispatches on nothing.",
}

// UseIsNaN flags a comparison against NaN.
//
//	valid:   Number.isNaN(value)
//	valid:   value === Number.NaN ? 0 : 1   (still reported; see below)
//	invalid: const NaN = 1; value === NaN   (a local shadowing the global IS reported; see below)
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
		// Defaults to on, matching ESLint 9 and this tree's config. An option that only relaxes the
		// rule gets the strict reading when the config says nothing, so a misconfiguration cannot
		// quietly disable half the rule.
		enforceForSwitchCase := true
		if settings, hasSettings := options.(UseIsNaNOptions); hasSettings && settings.EnforceForSwitchCase != nil {
			enforceForSwitchCase = *settings.EnforceForSwitchCase
		}

		return rule.Listeners{
			ast.KindSwitchStatement: func(node *ast.Node) {
				if !enforceForSwitchCase {
					return
				}

				statement := node.AsSwitchStatement()

				// Switching on NaN kills every case at once, so it is reported on the discriminant
				// rather than once per clause.
				if isNaNReference(statement.Expression) {
					ctx.ReportNode(statement.Expression, messageSwitchOnNaN)
					return
				}

				if statement.CaseBlock == nil {
					return
				}
				for _, clause := range statement.CaseBlock.AsCaseBlock().Clauses.Nodes {
					if clause.Kind != ast.KindCaseClause {
						continue
					}
					if label := clause.AsCaseOrDefaultClause().Expression; isNaNReference(label) {
						ctx.ReportNode(label, messageCaseWithNaN)
					}
				}
			},

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
// Matched on spelling rather than through the checker, so a local binding named NaN shadows the
// global and is reported anyway. That is a known false positive, accepted because declaring a
// binding called NaN is rare enough and strange enough that the trade favors catching the real
// defect.
//
// This comment previously claimed the opposite in two places: that a shadowed NaN was not reported,
// and that the fixtures covered the case. Neither was true. No such fixture existed, and a probe
// against this rule reported `comparisonWithNaN` on `const NaN = 1; value === NaN`. Found by a
// research pass on `no-regex-spaces`, a sibling asking the same shadowing question and reading this
// rule for precedent. The fixture below now pins the real behavior, so the next reader takes away
// what the code does rather than what this comment wished it did.
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
