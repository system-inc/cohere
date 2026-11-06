package core

import (
	"unicode"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageRedundantBooleanCall = rule.Message{
	Id: "redundantBooleanCall",
	Description: "This wraps a value in `Boolean(...)` somewhere the value is already coerced to a " +
		"boolean: an `if` test, a `while` test, the operand of `!`, the test of a ternary, or the " +
		"argument of another `Boolean` call. The call runs, allocates nothing useful, and produces " +
		"exactly what the surrounding position would have produced without it. Pass the value itself.",
}

var messageRedundantDoubleNegation = rule.Message{
	Id: "redundantDoubleNegation",
	Description: "This applies `!!` somewhere the value is already coerced to a boolean, so the " +
		"second negation undoes the first and the pair leaves the condition exactly as it found " +
		"it. Two operators that cancel read as if they were doing something. Drop them.",
}

// NoExtraBooleanCast flags a boolean cast in a position that already coerces to boolean.
//
//	valid:   var foo = !!bar;
//	valid:   var foo = Boolean(bar);
//	valid:   if (new Boolean(foo)) {}
//	valid:   if ((Boolean(1), 2)) {}
//	invalid: if (!!foo) {}
//	invalid: if (Boolean(foo)) {}
//	invalid: var foo = !!!bar;
//	invalid: var foo = Boolean(!!bar);
//
// # The two findings are not the same kind of finding
//
// Unwrapping `Boolean(x)` is a Fix and the engine applies it unattended, because in a position that
// already coerces, `Boolean(x)` and `x` are indistinguishable to everything downstream.
//
// Removing `!!` is a Suggestion instead. That looks inconsistent and it is deliberate: the two
// rewrites have different blast radii when the surrounding context turns out not to coerce after
// all. Upstream encodes the same split, and it is the only rule in oxc's 946 that declares
// `conditional_fix_or_conditional_suggestion`. Collapsing them into one kind is the mistake this
// comment exists to prevent, because a rule that auto-applies the `!!` removal is auto-applying a
// change of meaning wherever the context analysis is wrong.
//
// # Which positions coerce
//
// Directly: the test of `if`, `while`, `do...while`, `for`, and a ternary; the operand of `!`; and
// the first argument of `Boolean(...)` or `new Boolean(...)`. `new Boolean` counts as a *context*
// even though it is never itself reported, which is why `if (new Boolean(foo)) {}` is a valid case.
//
// Under the option, also transitively: both operands of `&&` and `||`, the right operand of `??`,
// both branches of a ternary, and the last element of a sequence, each only when the enclosing
// expression is itself in a coercing position. The recursion is what makes `if (!!foo || bar) {}`
// report while `var foo = bar || !!baz` stays silent.
//
// # Parentheses are part of the repair, not decoration
//
// The unwrapped expression lands where a call or a `!!` used to sit, and those bind tighter than
// most operators, so `!Boolean(a && b)` becomes `!(a && b)` rather than `!a && b`, which would mean
// something else entirely. The precedence comparison below is the whole of that decision.
var NoExtraBooleanCast = rule.Rule[NoExtraBooleanCastOptions]{
	Name: "no-extra-boolean-cast",
	Run: func(ruleContext rule.Context, options rule.Given[NoExtraBooleanCastOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}

		enforceForInnerExpressions := false
		if parsed, ok := options.Read(); ok {
			enforceForInnerExpressions = parsed.EnforceForInnerExpressions
		}

		reporter := booleanCastReporter{
			context:                    ruleContext,
			sourceText:                 ruleContext.SourceFile.Text(),
			enforceForInnerExpressions: enforceForInnerExpressions,
		}

		return rule.Listeners{
			ast.KindCallExpression:        reporter.reportBooleanCall,
			ast.KindPrefixUnaryExpression: reporter.reportDoubleNegation,
		}
	},
}

// removeDoubleNegation strips one `!!` pair from an expression, if it wears one.
//
// This is what turns `Boolean(!!foo)` into `Boolean(foo)` in a single report rather than needing the
// nested finding to fire first and the file to be linted twice.
func removeDoubleNegation(node *ast.Node) *ast.Node {
	inner := withoutNegation(node)
	if inner == nil {
		return node
	}
	innermost := withoutNegation(inner)
	if innermost == nil {
		return node
	}
	return innermost
}

// withoutNegation returns the operand of a `!`, or nil when the node is not one.
func withoutNegation(node *ast.Node) *ast.Node {
	skipped := ast.SkipParentheses(node)
	if skipped == nil || skipped.Kind != ast.KindPrefixUnaryExpression {
		return nil
	}
	unary := skipped.AsPrefixUnaryExpression()
	if unary == nil || unary.Operator != ast.KindExclamationToken {
		return nil
	}
	return unary.Operand
}

// isBooleanIdentifier answers whether an expression is the bare name `Boolean`.
//
// A name comparison and not a resolution, matching oxc. Under a locally-declared `function
// Boolean(x)` this reports and ESLint does not, because ESLint additionally asks whether the name
// resolves to the global. Reproduced rather than improved on: the divergence is upstream's, it is
// untested on either side, and closing it here would make this rule the only one in the package
// whose answer depends on scope analysis.
func isBooleanIdentifier(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindIdentifier {
		return false
	}
	return node.Text() == "Boolean"
}

// isBooleanCallOrConstruction answers whether a node is `Boolean(...)` or `new Boolean(...)`.
//
// `new Boolean(...)` counts as a coercing *context* while never being reported itself, which is why
// `if (new Boolean(foo)) {}` is valid and `new Boolean(!!foo)` is not.
func isBooleanCallOrConstruction(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallExpression:
		return isBooleanIdentifier(node.AsCallExpression().Expression)
	case ast.KindNewExpression:
		return isBooleanIdentifier(node.AsNewExpression().Expression)
	}
	return false
}

// callOrNew narrows a node to a call or construction, or nil.
func callOrNew(node *ast.Node) *ast.Node {
	if node != nil && (node.Kind == ast.KindCallExpression || node.Kind == ast.KindNewExpression) {
		return node
	}
	return nil
}

// argumentNodes returns a call's or construction's arguments, tolerating a missing list.
func argumentNodes(node *ast.Node) []*ast.Node {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case ast.KindCallExpression:
		if list := node.AsCallExpression().Arguments; list != nil {
			return list.Nodes
		}
	case ast.KindNewExpression:
		if list := node.AsNewExpression().Arguments; list != nil {
			return list.Nodes
		}
	}
	return nil
}

// isLogicalNot answers whether a node is a `!` expression.
func isLogicalNot(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindPrefixUnaryExpression {
		return false
	}
	unary := node.AsPrefixUnaryExpression()
	return unary != nil && unary.Operator == ast.KindExclamationToken
}

// isNullishCoalescing answers whether an expression is a `??` at its top level.
func isNullishCoalescing(node *ast.Node) bool {
	return binaryOperatorIs(node, ast.KindQuestionQuestionToken)
}

// isLogicalAndOr answers whether an expression is a `&&` or `||` at its top level.
func isLogicalAndOr(node *ast.Node) bool {
	return binaryOperatorIs(node, ast.KindAmpersandAmpersandToken) ||
		binaryOperatorIs(node, ast.KindBarBarToken)
}

// binaryOperatorIs answers whether a node is a binary expression with the given operator.
func binaryOperatorIs(node *ast.Node, operator ast.Kind) bool {
	if node == nil || node.Kind != ast.KindBinaryExpression {
		return false
	}
	binary := node.AsBinaryExpression()
	return binary != nil && binary.OperatorToken != nil && binary.OperatorToken.Kind == operator
}

// sameExpression answers whether a slot holds this node, looking through parentheses.
//
// Compared by identity after skipping parentheses, which is what upstream's node-id comparison does.
// Comparing spans instead would confuse `(a)` with `a` in a file where both appear at the same
// offsets, and comparing text would confuse any two identical subexpressions.
func sameExpression(slot *ast.Node, node *ast.Node) bool {
	if slot == nil || node == nil {
		return false
	}
	return ast.SkipParentheses(slot) == ast.SkipParentheses(node)
}

// isIdentifierPart answers whether a rune may continue an identifier.
func isIdentifierPart(character rune) bool {
	return character == '$' || character == '_' || unicode.IsLetter(character) ||
		unicode.IsDigit(character)
}

// isIdentifierStart answers whether a rune may begin an identifier.
func isIdentifierStart(character rune) bool {
	return character == '$' || character == '_' || unicode.IsLetter(character)
}

// isAsciiSpace answers whether a byte is ASCII whitespace.
func isAsciiSpace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
