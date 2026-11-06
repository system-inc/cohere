package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// consistentTypeAssertionsState carries what every step needs.
type consistentTypeAssertionsState struct {
	ruleContext rule.Context
	settings    ConsistentTypeAssertionsOptions
}

// textOf reads a node's own source, excluding leading trivia.
func (state *consistentTypeAssertionsState) textOf(node *ast.Node) string {
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)
	return state.ruleContext.SourceFile.Text()[nodeRange.Pos():nodeRange.End()]
}

// reportIncorrectStyle is upstream's `reportIncorrectAssertionType`.
func (state *consistentTypeAssertionsState) reportIncorrectStyle(
	node *ast.Node,
	expression *ast.Node,
	typeNode *ast.Node,
) {
	// `as const` is not an assertion anybody can rewrite, so `never` leaves it alone. The other two
	// styles still report it, because the spelling is the thing being enforced.
	if state.settings.AssertionStyle == ConsistentTypeAssertionsNever && isConstTypeNode(typeNode) {
		return
	}

	if state.settings.AssertionStyle == ConsistentTypeAssertionsNever {
		state.ruleContext.ReportNode(node, rule.Message{
			Id:          "never",
			Description: "Do not use any type assertions.",
		})
		return
	}

	cast := state.textOf(typeNode)
	if state.settings.AssertionStyle == ConsistentTypeAssertionsAngleBracket {
		state.ruleContext.ReportNode(node, rule.Message{
			Id:          "angle-bracket",
			Description: "Use '<" + cast + ">' instead of 'as " + cast + "'.",
		})
		return
	}

	// Only the angle-bracket-to-`as` direction is fixable upstream, and this is it.
	state.ruleContext.ReportNodeWithFixes(node, rule.Message{
		Id:          "as",
		Description: "Use 'as " + cast + "' instead of '<" + cast + ">'.",
	}, rule.ReplaceRange(rule.TokenRange(state.ruleContext.SourceFile, node),
		state.buildAsExpressionText(node, expression, cast)))
}

// buildAsExpressionText is upstream's fixer body.
//
// Two wrappings, decided independently. The operand is wrapped when it binds no tighter than `as`,
// and the whole result is wrapped when `as` binds no tighter than the parent, which is skipped when
// the assertion already sits inside parentheses and therefore needs none of its own.
func (state *consistentTypeAssertionsState) buildAsExpressionText(
	node *ast.Node,
	expression *ast.Node,
	cast string,
) string {
	asPrecedence := ast.GetOperatorPrecedence(ast.KindAsExpression, ast.KindUnknown,
		ast.OperatorPrecedenceFlagsNone)

	expressionText := wrapWhenLooser(state.textOf(expression),
		assertedExpressionPrecedence(expression), asPrecedence)
	text := expressionText + " as " + cast

	// An assertion already inside parentheses borrows them, so adding a second pair would be noise.
	// Upstream expresses this as `isParenthesized(node, sourceCode)`; our parser keeps the
	// parenthesis as a real node, so the same question is whether the parent is one.
	if node.Parent != nil && node.Parent.Kind == ast.KindParenthesizedExpression {
		return text
	}
	return wrapWhenLooser(text, asPrecedence, parentPrecedenceOf(node))
}

// checkLiteralAssertion is upstream's two literal-assertion checks.
//
// They differ only in which literal kind and which setting they read, so they are one function here
// with those two as parameters. The message ids stay distinct, because upstream reports a different
// one for each and the corpus asserts both.
func (state *consistentTypeAssertionsState) checkLiteralAssertion(
	node *ast.Node,
	expression *ast.Node,
	typeNode *ast.Node,
) {
	if expression == nil || typeNode == nil {
		return
	}

	switch expression.Kind {
	case ast.KindObjectLiteralExpression:
		state.reportLiteralAssertion(node, expression, typeNode,
			state.settings.ObjectLiteralTypeAssertion,
			"unexpectedObjectTypeAssertion", "Always prefer const x: T = { ... }.",
			"replaceObjectTypeAssertionWithAnnotation", "Use const x: {{cast}} = { ... } instead.",
			"replaceObjectTypeAssertionWithSatisfies", "Use const x = { ... } satisfies {{cast}} instead.")
	case ast.KindArrayLiteralExpression:
		state.reportLiteralAssertion(node, expression, typeNode,
			state.settings.ArrayLiteralTypeAssertion,
			"unexpectedArrayTypeAssertion", "Always prefer const x: T[] = [ ... ].",
			"replaceArrayTypeAssertionWithAnnotation", "Use const x: {{cast}} = [ ... ] instead.",
			"replaceArrayTypeAssertionWithSatisfies", "Use const x = [ ... ] satisfies {{cast}} instead.")
	}
}

// reportLiteralAssertion is the shared body of upstream's two literal checks.
func (state *consistentTypeAssertionsState) reportLiteralAssertion(
	node *ast.Node,
	expression *ast.Node,
	typeNode *ast.Node,
	setting ConsistentTypeAssertionsLiteralSetting,
	reportId string, reportText string,
	annotationId string, annotationTemplate string,
	satisfiesId string, satisfiesTemplate string,
) {
	if setting == ConsistentTypeAssertionsAllow {
		return
	}
	if setting == ConsistentTypeAssertionsAllowAsParameter && isAssertionInParameterPosition(node) {
		return
	}
	if !isCheckableAssertionType(typeNode) {
		return
	}

	cast := state.textOf(typeNode)
	state.ruleContext.ReportNodeWithSuggestions(node,
		rule.Message{Id: reportId, Description: reportText},
		state.buildLiteralSuggestions(node, expression, cast,
			annotationId, annotationTemplate, satisfiesId, satisfiesTemplate)...)
}

// buildLiteralSuggestions is upstream's `getSuggestions`.
//
// The annotation suggestion is offered only when the assertion initializes a declarator that carries
// no annotation of its own, because adding one where a type already sits would produce two. The
// satisfies suggestion is always offered.
func (state *consistentTypeAssertionsState) buildLiteralSuggestions(
	node *ast.Node,
	expression *ast.Node,
	cast string,
	annotationId string, annotationTemplate string,
	satisfiesId string, satisfiesTemplate string,
) []rule.Suggestion {
	// Upstream reads `getTextWithParentheses`, which keeps a parenthesized expression's own
	// parentheses. Our parser makes the parenthesis a node, so its text already carries them and
	// reading the node is the same answer.
	expressionText := state.textOf(expression)
	nodeRange := rule.TokenRange(state.ruleContext.SourceFile, node)

	var suggestions []rule.Suggestion
	if declarator := declaratorWithoutAnnotation(node); declarator != nil {
		nameRange := rule.TokenRange(state.ruleContext.SourceFile, declarator.AsVariableDeclaration().Name())
		suggestions = append(suggestions, rule.Suggestion{
			Message: rule.Message{
				Id:          annotationId,
				Description: renderConsistentTypeAssertionsCast(annotationTemplate, cast),
			},
			Fixes: []rule.Fix{
				rule.ReplaceRange(core.NewTextRange(nameRange.End(), nameRange.End()), ": "+cast),
				rule.ReplaceRange(nodeRange, expressionText),
			},
		})
	}

	suggestions = append(suggestions, rule.Suggestion{
		Message: rule.Message{
			Id:          satisfiesId,
			Description: renderConsistentTypeAssertionsCast(satisfiesTemplate, cast),
		},
		Fixes: []rule.Fix{
			rule.ReplaceRange(nodeRange, expressionText+" satisfies "+cast),
		},
	})
	return suggestions
}
