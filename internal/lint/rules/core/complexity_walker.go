package core

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// complexityWalker carries the frame stack across the recursive walk.
type complexityWalker struct {
	ruleContext rule.Context
	settings    ComplexitySettings
	frames      []complexityFrame
}

func (walker *complexityWalker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	// A class field's code path covers its initializer only, not the whole member, and the
	// difference is visible: a computed key is evaluated where the class is defined, so its
	// branches belong to the enclosing function rather than to the field. Measured against the
	// installed rule at max 1, `function foo() { class C { [x || y] = a; } }` reports on `foo`
	// with a complexity of 2, which is only true if the key counted outside the field's frame.
	//
	// So a property declaration is walked in two pieces rather than one: everything but the
	// initializer stays in the enclosing frame, and the initializer gets its own.
	if node.Kind == ast.KindPropertyDeclaration {
		declaration := node.AsPropertyDeclaration()
		if declaration != nil && declaration.Initializer != nil {
			node.ForEachChild(func(child *ast.Node) bool {
				if child == declaration.Initializer {
					return false
				}
				walker.walk(child)
				return false
			})
			walker.frames = append(walker.frames, complexityFrame{node: node, count: 1})
			walker.walk(declaration.Initializer)
			frame := walker.frames[len(walker.frames)-1]
			walker.frames = walker.frames[:len(walker.frames)-1]
			walker.reportIfOverThreshold(frame)
			return
		}
	}

	pushed := false
	if complexityStartsAFrame(node) {
		walker.frames = append(walker.frames, complexityFrame{node: node, count: 1})
		pushed = true
	} else {
		walker.countIfDecisionPoint(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		walker.walk(child)
		return false
	})

	if pushed {
		frame := walker.frames[len(walker.frames)-1]
		walker.frames = walker.frames[:len(walker.frames)-1]
		walker.reportIfOverThreshold(frame)
	}
}

// countIfDecisionPoint is upstream's `increaseComplexity` listener set.
//
// A node that starts a frame is never also counted, matching upstream: a nested function's own
// complexity belongs to its own code path rather than to its parent's.
func (walker *complexityWalker) countIfDecisionPoint(node *ast.Node) {
	if len(walker.frames) == 0 {
		// Upstream still counts into the `program` code path here; it simply never reports it,
		// because `onCodePathEnd` filters origins. Nothing to do.
		return
	}

	counts := false
	switch node.Kind {
	case ast.KindCatchClause, ast.KindConditionalExpression,
		ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement,
		ast.KindIfStatement, ast.KindWhileStatement, ast.KindDoStatement:
		counts = true

	// Upstream's LogicalExpression and its logical-assignment arm are one kind here, so both are
	// answered by looking at the operator.
	case ast.KindBinaryExpression:
		counts = complexityIsLogicalOperator(node)

	// Upstream's AssignmentPattern: a parameter default is a branch, because the parameter may or
	// may not be supplied. Our parser models it as an Initializer on the parameter rather than as
	// its own node, so the arm is on the parameter and gated on having one.
	case ast.KindParameter:
		declaration := node.AsParameterDeclaration()
		counts = declaration != nil && declaration.Initializer != nil

	// A destructuring default, which upstream also reaches through AssignmentPattern.
	case ast.KindBindingElement:
		element := node.AsBindingElement()
		counts = element != nil && element.Initializer != nil

	// Upstream's optional MemberExpression and CallExpression arms. Our parser marks the optional
	// chain with a token on the access rather than a boolean on the node.
	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		counts = access != nil && access.QuestionDotToken != nil
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		counts = access != nil && access.QuestionDotToken != nil
	case ast.KindCallExpression:
		call := node.AsCallExpression()
		counts = call != nil && call.QuestionDotToken != nil

	// The two variant-dependent arms, which are exclusive.
	case ast.KindCaseClause:
		// Upstream's `SwitchCase[test]`: only a case carrying a test counts, so `default` never
		// does -- and our parser gives `default` its own kind, so this arm cannot see one.
		counts = walker.settings.Variant != ComplexityModified
	case ast.KindSwitchStatement:
		counts = walker.settings.Variant == ComplexityModified
	}

	if counts {
		walker.frames[len(walker.frames)-1].count++
	}
}

// reportIfOverThreshold is upstream's `onCodePathEnd`.
func (walker *complexityWalker) reportIfOverThreshold(frame complexityFrame) {
	if frame.count <= walker.settings.Maximum {
		return
	}
	name := complexityNameOf(walker.ruleContext, frame.node)
	walker.ruleContext.Report(rule.Diagnostic{
		Range:      complexityHeadRange(walker.ruleContext, frame.node),
		SourceFile: walker.ruleContext.SourceFile,
		Message: rule.Message{
			Id: messageComplexityComplex.Id,
			Description: fmt.Sprintf("%s has a complexity of %d. Maximum allowed is %d. %s",
				name, frame.count, walker.settings.Maximum, messageComplexityComplex.Description),
		},
	})
}
