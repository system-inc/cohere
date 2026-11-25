package core

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// unusedExpressionVerdict answers whether one expression's value goes unused.
type unusedExpressionVerdict struct {
	options NoUnusedExpressionsOptions
}

// isDisallowed is upstream's `Checker.isDisallowed`, which reads inverted from its name: it answers
// true for the forms that have *no* effect and are therefore not allowed to stand alone.
//
// Upstream's default arm is "allowed", so an unrecognised node is left alone rather than reported.
// That default is load-bearing and is reproduced by the closing `return false` rather than by
// enumerating every used form: a node kind this port has never seen should go unreported, not
// reported on a guess.
func (verdict unusedExpressionVerdict) isDisallowed(expression *ast.Node) bool {
	switch expression.Kind {
	// Values with no effect at all. Every one of these is oxc's always-disallowed list, minus the
	// forms TypeScript spells with a different node.
	case ast.KindNumericLiteral,
		ast.KindBigIntLiteral,
		ast.KindStringLiteral,
		ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateExpression,
		ast.KindRegularExpressionLiteral,
		ast.KindTrueKeyword,
		ast.KindFalseKeyword,
		ast.KindNullKeyword,
		ast.KindIdentifier,
		ast.KindThisKeyword,
		ast.KindSuperKeyword,
		ast.KindMetaProperty,
		ast.KindArrayLiteralExpression,
		ast.KindObjectLiteralExpression,
		ast.KindFunctionExpression,
		ast.KindArrowFunction,
		ast.KindClassExpression,
		ast.KindPropertyAccessExpression,
		ast.KindElementAccessExpression:
		return true

	// Forms whose evaluation is the point. Listed rather than left to the default so that reading
	// this switch tells you what upstream considers used, and so that a later reader moving one out
	// of the default arm has to say why.
	//
	// A call covers more ground here than upstream's list suggests, because TypeScript has no
	// ChainExpression node: `a?.b()` is a call at the statement level, and `import('./foo')` is a
	// call too. oxc needs a ChainElement arm to reach the same answers.
	case ast.KindCallExpression,
		ast.KindNewExpression,
		ast.KindAwaitExpression,
		ast.KindYieldExpression,
		ast.KindPostfixUnaryExpression,
		ast.KindDeleteExpression,
		ast.KindVoidExpression,
		ast.KindSatisfiesExpression:
		return false

	// A prefix unary is `!a` and `+a`, which are disallowed, but also `++a`, which is an update and
	// is not. TypeScript gives all three one kind and puts the difference in the operator.
	case ast.KindPrefixUnaryExpression:
		operator := expression.AsPrefixUnaryExpression().Operator
		return operator != ast.KindPlusPlusToken && operator != ast.KindMinusMinusToken

	// `typeof a` is a unary that reads a value and discards it, which upstream reports. It is a
	// separate kind here rather than an operator on the unary node.
	case ast.KindTypeOfExpression:
		return true

	// The four-way collision. oxc and ESLint each get a distinct node type for assignment, sequence,
	// logical and arithmetic; TypeScript gives all four KindBinaryExpression and puts the
	// distinction in the operator token, so the discrimination has to happen here or every `a = b`
	// in the tree reports.
	case ast.KindBinaryExpression:
		binary := expression.AsBinaryExpression()
		operator := binary.OperatorToken.Kind
		switch {
		// An assignment has an effect. This arm has to be tested before the logical one, though the
		// two predicates turn out to be disjoint: `&&=` is an assignment operator and is not a
		// logical operator, measured rather than assumed.
		case ast.IsAssignmentOperator(operator):
			return false
		case ast.IsLogicalOrCoalescingBinaryOperator(operator):
			if verdict.options.AllowShortCircuit {
				return verdict.isDisallowed(binary.Right)
			}
			return true
		// A sequence and an arithmetic comparison are both always disallowed upstream, by separate
		// arms that reach the same answer. `f(), 0` reports as a whole even though `f()` is used.
		default:
			return true
		}

	case ast.KindConditionalExpression:
		if verdict.options.AllowTernary {
			conditional := expression.AsConditionalExpression()
			return verdict.isDisallowed(conditional.WhenTrue) || verdict.isDisallowed(conditional.WhenFalse)
		}
		return true

	case ast.KindTaggedTemplateExpression:
		return !verdict.options.AllowTaggedTemplates

	case ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment:
		return verdict.options.EnforceForJsx

	// The wrappers that carry no runtime meaning: parentheses and the type-only assertions. Each
	// hands the question to what it wraps, which is why `(a())` stays silent while `(a)` reports.
	case ast.KindParenthesizedExpression,
		ast.KindAsExpression,
		ast.KindTypeAssertionExpression,
		ast.KindNonNullExpression,
		ast.KindExpressionWithTypeArguments:
		return verdict.isDisallowed(expression.Expression())
	}

	return false
}
