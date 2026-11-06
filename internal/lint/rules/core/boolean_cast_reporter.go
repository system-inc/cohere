package core

import (
	"strings"
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// booleanCastReporter carries the per-file state the two listeners share.
type booleanCastReporter struct {
	context                    rule.Context
	sourceText                 string
	enforceForInnerExpressions bool
}

// reportBooleanCall handles `Boolean(x)` sitting in a coercing position.
func (reporter booleanCastReporter) reportBooleanCall(node *ast.Node) {
	call := node.AsCallExpression()
	if call == nil || !isBooleanIdentifier(call.Expression) {
		return
	}
	if !reporter.isFlaggedContext(node) {
		return
	}

	fix, hasFix := reporter.booleanCallFix(node)
	if !hasFix {
		reporter.context.ReportNode(node, messageRedundantBooleanCall)
		return
	}
	reporter.context.ReportNodeWithFixes(node, messageRedundantBooleanCall, fix)
}

// reportDoubleNegation handles the outer `!` of a `!!` pair in a coercing position.
//
// The listener fires on the *inner* negation and walks up, matching upstream, so the node reported
// is the outer `!` and its whole `!!x` span. Listening on the outer one instead would report the
// same finding but would have to look down through parentheses to recognise the pair.
func (reporter booleanCastReporter) reportDoubleNegation(node *ast.Node) {
	unary := node.AsPrefixUnaryExpression()
	if unary == nil || unary.Operator != ast.KindExclamationToken {
		return
	}

	parent := reporter.realParent(node)
	if parent == nil || parent.Kind != ast.KindPrefixUnaryExpression {
		return
	}
	outer := parent.AsPrefixUnaryExpression()
	if outer == nil || outer.Operator != ast.KindExclamationToken {
		return
	}
	if !reporter.isFlaggedContext(parent) {
		return
	}

	argument := ast.SkipParentheses(unary.Operand)
	replacement := reporter.replacementText(argument, parent, reporter.tokenText(argument))
	reporter.context.ReportNodeWithSuggestions(parent, messageRedundantDoubleNegation,
		rule.Suggestion{
			Message: messageRedundantDoubleNegation,
			Fixes:   []rule.Fix{reporter.context.ReplaceNode(parent, replacement)},
		})
}

// booleanCallFix builds the repair for a `Boolean(...)` call, or declines to.
//
// Three shapes, and the argument-less one is the reason this returns a flag rather than a string.
// `Boolean()` is `false`, so in a coercing position it collapses to the literal, and under a `!` the
// whole `!Boolean()` collapses to `true` instead. More than one argument, or a spread, is declined
// outright: `Boolean(...foo)` cannot be reduced to any single expression without knowing what `foo`
// holds, so upstream emits the finding with no repair and so does this.
func (reporter booleanCastReporter) booleanCallFix(node *ast.Node) (rule.Fix, bool) {
	arguments := argumentNodes(node)

	if len(arguments) == 0 {
		if parent := reporter.realParent(node); parent != nil && isLogicalNot(parent) {
			replacement := reporter.padForTokenBoundary(parent, "true")
			return reporter.context.ReplaceNode(parent, replacement), true
		}
		return reporter.context.ReplaceNode(node, "false"), true
	}

	if len(arguments) != 1 || arguments[0].Kind == ast.KindSpreadElement {
		return rule.Fix{}, false
	}

	argument := ast.SkipParentheses(removeDoubleNegation(arguments[0]))
	replacement := reporter.replacementText(argument, node, reporter.tokenText(argument))
	return reporter.context.ReplaceNode(node, replacement), true
}

// isFlaggedContext answers whether a cast at this node sits somewhere that coerces.
//
// Directly, or transitively through the inner-expression forms when the option is on. The recursion
// asks the same question of the parent, so `if (!!a || b) {}` reaches the `if` test through the
// `||` while `var x = !!a || b` runs out of coercing parents and stays silent.
func (reporter booleanCastReporter) isFlaggedContext(node *ast.Node) bool {
	parent := reporter.realParent(node)
	if reporter.isBooleanContext(node, parent) {
		return true
	}
	if parent == nil {
		return false
	}
	return reporter.isInnerBooleanContext(node, parent) && reporter.isFlaggedContext(parent)
}

// isBooleanContext answers whether the parent coerces this node directly.
func (reporter booleanCastReporter) isBooleanContext(node *ast.Node, parent *ast.Node) bool {
	if parent == nil {
		return false
	}
	if isBooleanCallOrConstruction(parent) && reporter.isFirstArgument(node, parent) {
		return true
	}
	if reporter.isTestPosition(node, parent) {
		return true
	}
	return isLogicalNot(parent)
}

// isInnerBooleanContext answers whether the parent *passes through* coercion to this node.
//
// Gated on the option, and each arm is the position whose value becomes the enclosing expression's
// value. `&&` and `||` pass through from either side. `??` passes through only on the right, because
// the left is tested for nullishness rather than truthiness. A ternary passes through its branches
// but not its test, which is already a direct boolean context above. A sequence passes through only
// its last element, which is what makes `if ((Boolean(1), 2)) {}` a valid case.
func (reporter booleanCastReporter) isInnerBooleanContext(node *ast.Node, parent *ast.Node) bool {
	if !reporter.enforceForInnerExpressions {
		return false
	}

	switch parent.Kind {
	case ast.KindBinaryExpression:
		binary := parent.AsBinaryExpression()
		if binary == nil || binary.OperatorToken == nil {
			return false
		}
		switch binary.OperatorToken.Kind {
		case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken:
			return true
		case ast.KindQuestionQuestionToken:
			return sameExpression(binary.Right, node)
		case ast.KindCommaToken:
			// A sequence in this tree is a left-nested chain of comma operators, so the "last
			// element" is the right side of the outermost comma and nothing else.
			return sameExpression(binary.Right, node)
		}
		return false

	case ast.KindConditionalExpression:
		conditional := parent.AsConditionalExpression()
		if conditional == nil {
			return false
		}
		return sameExpression(conditional.WhenTrue, node) || sameExpression(conditional.WhenFalse, node)
	}
	return false
}

// isTestPosition answers whether the node is the tested expression of a statement or ternary.
func (reporter booleanCastReporter) isTestPosition(node *ast.Node, parent *ast.Node) bool {
	var test *ast.Node
	switch parent.Kind {
	case ast.KindIfStatement:
		test = parent.AsIfStatement().Expression
	case ast.KindWhileStatement:
		test = parent.AsWhileStatement().Expression
	case ast.KindDoStatement:
		test = parent.AsDoStatement().Expression
	case ast.KindForStatement:
		test = parent.AsForStatement().Condition
	case ast.KindConditionalExpression:
		test = parent.AsConditionalExpression().Condition
	default:
		return false
	}
	return sameExpression(test, node)
}

// isFirstArgument answers whether the node is the first argument of a call or construction.
func (reporter booleanCastReporter) isFirstArgument(node *ast.Node, parent *ast.Node) bool {
	arguments := argumentNodes(callOrNew(parent))
	if len(arguments) == 0 {
		return false
	}
	return sameExpression(arguments[0], node)
}

// realParent walks up past parentheses, which upstream also skips.
//
// Parentheses are a node in this tree exactly as they are in oxc's, and every question this rule
// asks is about the expression structure rather than the written grouping: `!(Boolean())` has to see
// the `!` as the parent of the call, or its repair produces `!false` instead of `true`.
func (reporter booleanCastReporter) realParent(node *ast.Node) *ast.Node {
	parent := node.Parent
	for parent != nil && parent.Kind == ast.KindParenthesizedExpression {
		parent = parent.Parent
	}
	return parent
}

// replacementText renders the unwrapped expression, parenthesised when precedence demands it.
func (reporter booleanCastReporter) replacementText(
	replacement *ast.Node,
	target *ast.Node,
	expressionText string,
) string {
	result := expressionText
	if reporter.needsParentheses(replacement, target) {
		trimmed := text.TrimWhitespace(expressionText)
		if !(strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")")) {
			result = "(" + expressionText + ")"
		}
	}
	return reporter.padForTokenBoundary(target, result)
}

// needsParentheses answers whether dropping the cast would re-associate the expression.
//
// The comparison is against the *parent of the thing being replaced*, because that is what the
// unwrapped expression will now bind to. Already-parenthesised targets need nothing: the source's
// own parentheses survive the replacement and do the job.
func (reporter booleanCastReporter) needsParentheses(replacement *ast.Node, target *ast.Node) bool {
	if target.Parent != nil && target.Parent.Kind == ast.KindParenthesizedExpression {
		return false
	}
	parent := reporter.realParent(target)
	if parent == nil {
		return false
	}
	replacementPrecedence := ast.GetExpressionPrecedence(replacement)

	switch parent.Kind {
	case ast.KindPrefixUnaryExpression:
		return isLogicalNot(parent) && replacementPrecedence < ast.OperatorPrecedenceUnary

	case ast.KindCallExpression, ast.KindNewExpression:
		// Only a comma expression, because an argument list is itself comma-separated and an
		// unparenthesised sequence would read as two arguments.
		return reporter.isFirstArgument(target, parent) &&
			replacementPrecedence == ast.OperatorPrecedenceComma

	case ast.KindConditionalExpression:
		conditional := parent.AsConditionalExpression()
		if conditional == nil {
			return false
		}
		if sameExpression(conditional.Condition, target) {
			return replacementPrecedence <= ast.OperatorPrecedenceConditional
		}
		if sameExpression(conditional.WhenTrue, target) || sameExpression(conditional.WhenFalse, target) {
			return replacementPrecedence == ast.OperatorPrecedenceComma
		}
		return false

	case ast.KindBinaryExpression:
		return reporter.binaryNeedsParentheses(replacement, target, parent)
	}
	return false
}

// binaryNeedsParentheses is the `&&`, `||`, `??` arm of needsParentheses.
//
// Split out because it is the only arm that has to reason about the operator token rather than the
// node kind, and about a mixing rule that is a syntax error rather than a precedence question.
func (reporter booleanCastReporter) binaryNeedsParentheses(
	replacement *ast.Node,
	target *ast.Node,
	parent *ast.Node,
) bool {
	binary := parent.AsBinaryExpression()
	if binary == nil || binary.OperatorToken == nil {
		return false
	}
	// Only the logical operators. Upstream reaches this arm solely through `LogicalExpression`,
	// which in this tree is folded into `BinaryExpression` alongside arithmetic, comparison, comma
	// and assignment, so the narrowing that oxc gets from its node kind has to be written out.
	operator := binary.OperatorToken.Kind
	if operator != ast.KindAmpersandAmpersandToken && operator != ast.KindBarBarToken &&
		operator != ast.KindQuestionQuestionToken {
		return false
	}

	replacementPrecedence := ast.GetExpressionPrecedence(replacement)
	operatorPrecedence := ast.GetBinaryOperatorPrecedence(operator)

	if replacementPrecedence < operatorPrecedence {
		return true
	}
	// Equal precedence is fine on the left and needs parentheses on the right, because these
	// operators are left-associative: `a || (b || c)` and `a || b || c` group differently.
	if replacementPrecedence == operatorPrecedence && sameExpression(binary.Right, target) {
		return true
	}
	// `??` cannot be mixed with `&&` or `||` without parentheses in either direction. That is a
	// grammar restriction rather than a precedence one, so it is checked by operator identity and
	// not by comparing numbers, which in this tree cannot even express it: `??` and `||` share the
	// precedence value 5, so the comparisons above see them as equal and let both through.
	if isNullishCoalescing(replacement) {
		return true
	}
	if operator == ast.KindQuestionQuestionToken && isLogicalAndOr(replacement) {
		return true
	}
	return false
}

// padForTokenBoundary inserts a space when the replacement would weld onto a neighbouring token.
//
// `void!Boolean()` becomes `void true` and not `voidtrue`, and `yield!!a ? b : c` becomes
// `yield a ? b : c` and not `yielda ? b : c`. The cast being removed was itself the separator, so
// deleting it is what creates the collision.
func (reporter booleanCastReporter) padForTokenBoundary(target *ast.Node, replacement string) string {
	if replacement == "" {
		return replacement
	}
	span := rule.TokenRange(reporter.context.SourceFile, target)
	start, end := span.Pos(), span.End()
	padded := replacement

	// Decoded as runes rather than indexed as bytes. Upstream indexes bytes and casts, which reads
	// a continuation byte as a character whenever the neighbour is non-ASCII, and every identifier
	// character above U+007F is multi-byte in UTF-8. Nothing in the imported corpus is non-ASCII so
	// no fixture here can see the difference, which is exactly why it is worth writing correctly:
	// the tree has identifiers with accented letters in it and the byte version would answer them
	// wrong in whichever direction the continuation byte happened to fall.
	previous, previousSize := utf8.DecodeLastRuneInString(reporter.sourceText[:start])
	if previousSize > 0 && isIdentifierPart(previous) {
		if first, _ := utf8.DecodeRuneInString(padded); isIdentifierPart(first) {
			padded = " " + padded
		}
	}
	next, nextSize := utf8.DecodeRuneInString(reporter.sourceText[end:])
	if nextSize > 0 && isIdentifierStart(next) &&
		!isAsciiSpace(padded[len(padded)-1]) {
		padded += " "
	}
	return padded
}

// tokenText returns a node's own source text, without the trivia in front of it.
func (reporter booleanCastReporter) tokenText(node *ast.Node) string {
	span := rule.TokenRange(reporter.context.SourceFile, node)
	return reporter.sourceText[span.Pos():span.End()]
}
