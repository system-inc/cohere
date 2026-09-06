package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/rule"
)

func messageInvalidTypeofValue(found string) rule.Message {
	return rule.Message{
		Id: "invalidTypeofValue",
		Description: "A typeof expression can only produce one of eight strings, and \"" + found +
			"\" is not among them, so this comparison is always false and the branch behind it is " +
			"dead. The eight are undefined, object, boolean, number, string, function, symbol and " +
			"bigint. Almost every finding here is a typo or a guess at a type name that the " +
			"language does not report: typeof null is \"object\", an array is \"object\", and " +
			"there is no \"array\", \"null\" or \"date\".",
	}
}

// typeofResults are the only strings a typeof expression can produce.
var typeofResults = map[string]bool{
	"undefined": true, "object": true, "boolean": true, "number": true,
	"string": true, "function": true, "symbol": true, "bigint": true,
}

// ValidTypeof flags a typeof comparison against a string it can never equal.
//
//	valid:   typeof value === 'string'
//	valid:   typeof value === typeof other
//	valid:   typeof value === someVariable
//	invalid: typeof value === 'strng'
//	invalid: typeof value === 'array'
//	invalid: typeof value === 'null'
//
// Like use-isnan, this catches a check that cannot succeed rather than one that is merely unusual,
// so a finding is nearly always a real defect. The three most common are a misspelling, `'array'`
// (an array reports as `'object'`), and `'null'` (null also reports as `'object'`, which is the
// language's oldest wart).
//
// Only literal comparisons are judged. `typeof a === typeof b` and `typeof a === someName` are
// legitimate and unanalyzable without the checker, so they are left alone.
//
// No fix. The rule knows the comparison is dead and cannot know which of the eight was meant, and
// the two most common cases want a different check entirely: `'array'` wants Array.isArray and
// `'null'` wants `value === null`.
var ValidTypeof = rule.Rule{
	Name: "valid-typeof",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindBinaryExpression: func(node *ast.Node) {
				binary := node.AsBinaryExpression()
				if binary.OperatorToken == nil {
					return
				}
				switch binary.OperatorToken.Kind {
				case ast.KindEqualsEqualsToken, ast.KindEqualsEqualsEqualsToken,
					ast.KindExclamationEqualsToken, ast.KindExclamationEqualsEqualsToken:
				default:
					return
				}

				// Either side may hold the typeof, so the other side is the one to judge.
				switch {
				case isTypeofExpression(binary.Left):
					reportUnreachableTypeofComparison(ctx, binary.Right)
				case isTypeofExpression(binary.Right):
					reportUnreachableTypeofComparison(ctx, binary.Left)
				}
			},
		}
	},
}

// reportUnreachableTypeofComparison reports when the compared operand is a string literal that
// typeof can never produce. Anything that is not a plain string literal is left alone, since only
// the checker could say what it holds.
func reportUnreachableTypeofComparison(ctx rule.Context, operand *ast.Node) {
	operand = unwrapParentheses(operand)
	if operand == nil {
		return
	}

	var text string
	switch operand.Kind {
	case ast.KindStringLiteral:
		text = operand.Text()
	case ast.KindNoSubstitutionTemplateLiteral:
		// A template with no substitutions is a string literal wearing backticks, and the same
		// typo is just as dead in that spelling.
		text = operand.Text()
	default:
		return
	}

	if !typeofResults[text] {
		ctx.ReportNode(operand, messageInvalidTypeofValue(text))
	}
}

// isTypeofExpression reports a typeof operator, looking through parentheses.
func isTypeofExpression(node *ast.Node) bool {
	node = unwrapParentheses(node)
	return node != nil && node.Kind == ast.KindTypeOfExpression
}

// unwrapParentheses peels grouping, which changes nothing about what an expression is.
func unwrapParentheses(node *ast.Node) *ast.Node {
	for node != nil && node.Kind == ast.KindParenthesizedExpression {
		node = node.AsParenthesizedExpression().Expression
	}
	return node
}
