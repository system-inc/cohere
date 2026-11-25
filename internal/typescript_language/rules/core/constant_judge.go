package core

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// constantJudge carries the one question the judgments below need of the program: whether a name is
// the global rather than a local that shares its spelling
type constantJudge struct {
	isGlobal func(identifier *ast.Node) bool
}

// isGlobalBooleanCall is isConstantBooleanCall asked of the global `Boolean` only, so a local function
// named Boolean returns whatever it returns
func (judge constantJudge) isGlobalBooleanCall(node *ast.Node) bool {
	callee := ast.SkipParentheses(node.AsCallExpression().Expression)
	return callee != nil && judge.isGlobal(callee) && isConstantBooleanCall(node)
}

// constantComparisonOperand returns the operand whose comparison against the other is fixed.
//
// Two ways that happens, and they are different questions. Comparing against null or undefined asks
// whether the other side's nullishness is fixed: an array is never nullish, so `a === null` on an
// array is always false. Comparing against a boolean asks whether the other side's coercion to
// boolean is fixed, which is a different and looser question under `==` than under `===`.
func (judge constantJudge) constantComparisonOperand(known *ast.Node, other *ast.Node, strict bool) *ast.Node {
	if judge.isNullishValue(known) && judge.hasConstantNullishness(other, false) {
		return other
	}
	if judge.isStaticBoolean(known) {
		if strict {
			if judge.hasConstantStrictBooleanComparison(other) {
				return other
			}
		} else if judge.hasConstantLooseBooleanComparison(other) {
			return other
		}
	}
	return nil
}

// isNullishValue reports the two nullish values, however they are spelled.
//
// Named apart from prefer_spread's isNullOrUndefined rather than merged with it, because the two
// ask different questions despite the near-identical name. That one is looking for "no receiver"
// in a `.call(null, ...)` position and accepts only `void 0`; this one is asking whether a value is
// nullish and accepts any `void`, since `void anything` evaluates to undefined. Merging them would
// widen a rule nobody asked me to change.
func (judge constantJudge) isNullishValue(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}
	switch unwrapped.Kind {
	case ast.KindNullKeyword, ast.KindVoidExpression:
		return true
	case ast.KindIdentifier:
		return unwrapped.Text() == "undefined" && judge.isGlobal(unwrapped)
	}
	return false
}

// isStaticBoolean reports a literal true or false, or a Boolean call with a constant argument.
func (judge constantJudge) isStaticBoolean(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}
	switch unwrapped.Kind {
	case ast.KindTrueKeyword, ast.KindFalseKeyword:
		return true
	case ast.KindCallExpression:
		return judge.isGlobalBooleanCall(unwrapped)
	}
	return false
}

// hasConstantNullishness reports whether an expression is always nullish or always is not.
//
// nonNullish says the caller has already established the node is not itself null or undefined,
// which is what stops `null ?? a` reporting on a left side that is exactly the value `??` exists to
// handle.
func (judge constantJudge) hasConstantNullishness(node *ast.Node, nonNullish bool) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}
	if nonNullish && judge.isNullishValue(unwrapped) {
		return false
	}

	switch unwrapped.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindClassExpression,
		ast.KindNewExpression,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindRegularExpressionLiteral, ast.KindNoSubstitutionTemplateLiteral,
		ast.KindTemplateExpression,
		ast.KindPostfixUnaryExpression:
		// Objects, arrays, functions, classes, constructed values, literals, template strings, and
		// the result of ++ or -- are never nullish, and a nullish literal is always nullish. Either
		// way the answer does not vary.
		return true

	case ast.KindIdentifier:
		return unwrapped.Text() == "undefined" && judge.isGlobal(unwrapped)

	case ast.KindVoidExpression, ast.KindTypeOfExpression, ast.KindPrefixUnaryExpression,
		ast.KindDeleteExpression, ast.KindAwaitExpression:
		// void is always undefined; typeof is always a string; the unary math and logical
		// operators all yield numbers, strings, or booleans. Await is ESLint's addition and is
		// deliberately not claimed here, so it falls through to false below.
		switch unwrapped.Kind {
		case ast.KindAwaitExpression:
			return false
		}
		return true

	case ast.KindCallExpression:
		// The five wrapper constructors always return a value of their own type.
		call := unwrapped.AsCallExpression()
		callee := ast.SkipParentheses(call.Expression)
		if callee == nil || callee.Kind != ast.KindIdentifier {
			return false
		}
		if !judge.isGlobal(callee) {
			return false
		}
		switch callee.Text() {
		case "Boolean", "String", "Number", "Symbol", "BigInt":
			return true
		}
		return false

	case ast.KindBinaryExpression:
		binary := unwrapped.AsBinaryExpression()
		if binary.OperatorToken == nil {
			return false
		}
		switch binary.OperatorToken.Kind {
		case ast.KindQuestionQuestionToken:
			// `a ?? b` is nullish exactly when b is, since a nullish a yields b.
			return judge.hasConstantNullishness(binary.Right, true)
		case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken:
			// Either operand may be the result, so nothing fixed can be said.
			return false
		case ast.KindEqualsToken:
			return judge.hasConstantNullishness(binary.Right, nonNullish)
		case ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken,
			ast.KindQuestionQuestionEqualsToken:
			// A logical assignment yields one side or the other; reasoning about it needs the
			// variable's value, which ESLint declines to do and so do we.
			return false
		}
		// Every other binary operator yields a number, string, or boolean.
		return true
	}

	return false
}

// hasConstantLooseBooleanComparison reports whether `x == true` always gives the same answer.
//
// Looser than the strict version because `==` coerces, so the question is whether the coercion is
// fixed rather than whether the node can be a boolean at all. `[]` coerces to `0`, `[1]` coerces to
// `1`, so an array of one element is not fixed while an empty one or a longer one is.
func (judge constantJudge) hasConstantLooseBooleanComparison(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}

	switch unwrapped.Kind {
	case ast.KindObjectLiteralExpression, ast.KindClassExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindRegularExpressionLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return true

	case ast.KindIdentifier:
		return unwrapped.Text() == "undefined" && judge.isGlobal(unwrapped)

	case ast.KindArrayLiteralExpression:
		// `[x]` coerces to whatever x stringifies to, which could be "0" or "1", so a single
		// element is the one case that varies. Empty or several elements always coerce to
		// something that is neither.
		elements := unwrapped.AsArrayLiteralExpression().Elements.Nodes
		nonSpread := 0
		for _, element := range elements {
			if element != nil && element.Kind != ast.KindSpreadElement {
				nonSpread++
			}
		}
		return len(elements) == 0 || nonSpread > 1

	case ast.KindTemplateExpression:
		// A template with substitutions could produce "0" or "1".
		return len(unwrapped.AsTemplateExpression().TemplateSpans.Nodes) == 0

	case ast.KindVoidExpression, ast.KindTypeOfExpression:
		return true

	case ast.KindPrefixUnaryExpression:
		unary := unwrapped.AsPrefixUnaryExpression()
		if unary.Operator == ast.KindExclamationToken {
			return isConstantExpression(unary.Operand, true)
		}
		// Not reasoned about: +, -, and ~ could coerce to 0 or 1.
		return false

	case ast.KindNewExpression:
		// A constructed object may define valueOf or toString.
		return false

	case ast.KindCallExpression:
		return judge.isGlobalBooleanCall(unwrapped)

	case ast.KindBinaryExpression:
		binary := unwrapped.AsBinaryExpression()
		if binary.OperatorToken == nil {
			return false
		}
		switch binary.OperatorToken.Kind {
		case ast.KindCommaToken:
			return judge.hasConstantLooseBooleanComparison(binary.Right)
		case ast.KindEqualsToken:
			return judge.hasConstantLooseBooleanComparison(binary.Right)
		}
		return false
	}

	return false
}

// hasConstantStrictBooleanComparison reports whether `x === true` always gives the same answer.
//
// The question is simply whether the node can ever be a boolean at all: an array is never a
// boolean, so `[] === true` is always false whatever the array holds. That makes it broader than
// the loose version rather than narrower, which reads backwards until you see why.
func (judge constantJudge) hasConstantStrictBooleanComparison(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}

	switch unwrapped.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindClassExpression,
		ast.KindNewExpression, ast.KindTemplateExpression,
		ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindRegularExpressionLiteral, ast.KindNoSubstitutionTemplateLiteral,
		ast.KindPostfixUnaryExpression,
		ast.KindVoidExpression, ast.KindTypeOfExpression:
		return true

	case ast.KindIdentifier:
		return unwrapped.Text() == "undefined" && judge.isGlobal(unwrapped)

	case ast.KindPrefixUnaryExpression:
		unary := unwrapped.AsPrefixUnaryExpression()
		if unary.Operator == ast.KindExclamationToken {
			return isConstantExpression(unary.Operand, true)
		}
		// The math operators all yield numbers, which are never booleans.
		return true

	case ast.KindDeleteExpression:
		// delete yields a boolean, so it can be one.
		return false

	case ast.KindCallExpression:
		call := unwrapped.AsCallExpression()
		callee := ast.SkipParentheses(call.Expression)
		if callee == nil || callee.Kind != ast.KindIdentifier {
			return false
		}
		if !judge.isGlobal(callee) {
			return false
		}
		switch callee.Text() {
		case "String", "Number", "BigInt", "Symbol":
			// Never return a boolean.
			return true
		case "Boolean":
			return isConstantBooleanCall(unwrapped)
		}
		return false

	case ast.KindBinaryExpression:
		binary := unwrapped.AsBinaryExpression()
		if binary.OperatorToken == nil {
			return false
		}
		switch binary.OperatorToken.Kind {
		case ast.KindCommaToken, ast.KindEqualsToken:
			return judge.hasConstantStrictBooleanComparison(binary.Right)
		case ast.KindBarBarEqualsToken, ast.KindAmpersandAmpersandEqualsToken,
			ast.KindQuestionQuestionEqualsToken:
			return false
		}
		return isNumericOrStringBinaryOperator(binary.OperatorToken.Kind)
	}

	return false
}

// isAlwaysNew reports whether an expression builds a fresh value every time it runs.
//
// Fresh means identity comparison can never succeed against anything, which is what makes
// `a === []` always false. A regular expression counts: it is an object.
//
// A `new` call counts only for the built-in constructors. A user-defined one may return a sentinel
// from its constructor, which is a real pattern and would make the comparison meaningful.
func (judge constantJudge) isAlwaysNew(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}

	switch unwrapped.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression,
		ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindClassExpression,
		ast.KindRegularExpressionLiteral:
		return true

	case ast.KindNewExpression:
		callee := ast.SkipParentheses(unwrapped.AsNewExpression().Expression)
		if callee == nil || callee.Kind != ast.KindIdentifier {
			return false
		}
		return isEcmaScriptGlobalConstructor(callee.Text()) && judge.isGlobal(callee)

	case ast.KindBinaryExpression:
		binary := unwrapped.AsBinaryExpression()
		if binary.OperatorToken == nil {
			return false
		}
		switch binary.OperatorToken.Kind {
		case ast.KindCommaToken, ast.KindEqualsToken:
			return judge.isAlwaysNew(binary.Right)
		}
		return false

	case ast.KindConditionalExpression:
		conditional := unwrapped.AsConditionalExpression()
		return judge.isAlwaysNew(conditional.WhenTrue) && judge.isAlwaysNew(conditional.WhenFalse)
	}

	return false
}

// isStaticLiteral reports a value knowable without running anything.
func (judge constantJudge) isStaticLiteral(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	if unwrapped == nil {
		return false
	}

	switch unwrapped.Kind {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword,
		ast.KindRegularExpressionLiteral, ast.KindNoSubstitutionTemplateLiteral:
		return true

	case ast.KindIdentifier:
		return unwrapped.Text() == "undefined" && judge.isGlobal(unwrapped)

	case ast.KindTemplateExpression:
		return len(unwrapped.AsTemplateExpression().TemplateSpans.Nodes) == 0

	case ast.KindPrefixUnaryExpression:
		unary := unwrapped.AsPrefixUnaryExpression()
		switch unary.Operator {
		case ast.KindMinusToken, ast.KindPlusToken, ast.KindTildeToken:
			operand := ast.SkipParentheses(unary.Operand)
			return operand != nil &&
				(operand.Kind == ast.KindNumericLiteral || operand.Kind == ast.KindBigIntLiteral ||
					operand.Kind == ast.KindStringLiteral)
		}
	}

	return false
}
