package typescript

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// parameterPropertyAssignmentVisitor walks a constructor body the way upstream's visitor does.
//
// Written as an explicit descent rather than as a listener plus an ancestor walk, because the two
// stopping conditions are properties of the descent itself: an assignment expression is never
// descended into at all, and a function-like that rebinds `this` ends the walk. An ancestor walk
// asking "how many functions am I inside" cannot express the first of those, and the census records
// a sibling rule shipping a live false positive from exactly that substitution.
type parameterPropertyAssignmentVisitor struct {
	context                   rule.Context
	parameterProperties       map[string]bool
	assignedBeforeUnnecessary map[string]bool
	assignedBeforeConstructor map[string]bool
}

func (visitor *parameterPropertyAssignmentVisitor) visit(node *ast.Node) {
	if node == nil {
		return
	}

	switch node.Kind {
	// Upstream's `visit_function` override. Each of these rebinds `this`, so an assignment inside
	// one writes to a different receiver and says nothing about this constructor's parameter
	// property. An arrow function is deliberately absent: it closes over `this` and upstream walks
	// into it.
	//
	// A nested constructor belongs here and its absence was a real defect, caught by upstream case
	// 16 and by nothing else. Upstream's `MethodDefinition` holds a `Function` as its value, so a
	// nested class's constructor is stopped by the same override that stops a method. Without this
	// arm the descent reports the inner assignment, and then the rule's own KindConstructor
	// listener reports it a second time when the walk reaches `class Bar` on its own: three
	// findings where upstream produces two, with the extra one sitting on top of a correct one so
	// it reads as a duplicate rather than as a wrong span.
	case ast.KindFunctionDeclaration,
		ast.KindFunctionExpression,
		ast.KindMethodDeclaration,
		ast.KindConstructor,
		ast.KindGetAccessor,
		ast.KindSetAccessor:
		return

	case ast.KindBinaryExpression:
		if ast.IsAssignmentOperator(node.AsBinaryExpression().OperatorToken.Kind) {
			visitor.visitAssignment(node)
			// Upstream's override returns on every path without calling the default walker, so an
			// assignment is a dead end and neither side is descended into. This is what makes
			// `this.bar = () => { this.foo = foo; }` clean, rather than any property of arrows.
			return
		}
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visitor.visit(child)
		return false
	})
}

func (visitor *parameterPropertyAssignmentVisitor) visitAssignment(node *ast.Node) {
	assignment := node.AsBinaryExpression()

	propertyName, ok := thisPropertyNameOfAssignmentTarget(assignment.Left)
	if !ok {
		return
	}

	if !isUnnecessaryAssignmentOperator(assignment.OperatorToken.Kind) {
		// A compound arithmetic assignment reads the property, so the parameter property's value is
		// now load-bearing and every later plain assignment to the same name is necessary.
		visitor.assignedBeforeUnnecessary[propertyName] = true
		return
	}

	right := skipOuterExpressions(assignment.Right)
	if right == nil || right.Kind != ast.KindIdentifier {
		return
	}
	if right.Text() != propertyName {
		return
	}
	if !visitor.parameterProperties[propertyName] {
		return
	}

	// Upstream additionally asks the semantic layer whether the identifier on the right resolves to
	// the parameter's own binding, which is what makes the block-shadow case clean:
	//
	//	constructor(public foo: number) { { const foo = 1; this.foo = foo; } }
	//
	// We have no reference index, so the same question is answered structurally by walking out from
	// the identifier looking for an intervening declaration of that name. See the helper.
	if identifierIsShadowedBefore(right, propertyName) {
		return
	}

	if visitor.assignedBeforeUnnecessary[propertyName] {
		return
	}
	if visitor.assignedBeforeConstructor[propertyName] {
		return
	}

	// Anchored on the reported range rather than on `node.Pos()`. A node's `Pos()` is the position
	// before its leading trivia, so a deletion starting there swallows the indentation of the line
	// as well as the assignment, and every one of upstream's 21 fix vectors keeps that indentation.
	// `rule.TokenRange` is the same trivia-stripped span the diagnostic itself carries, which is
	// also what makes the finding and its repair point at the same bytes.
	reported := rule.TokenRange(visitor.context.SourceFile, node)

	visitor.context.ReportNodeWithSuggestions(node, messageNoUnnecessaryParameterPropertyAssignment,
		rule.Suggestion{
			Message: suggestionNoUnnecessaryParameterPropertyAssignment,
			Fixes: []rule.Fix{{
				// Upstream's `Span::new(span.start, span.end + 1)`, which reaches one byte past the
				// expression to take the semicolon with it. The blind byte is documented at the
				// rule and is why this is a suggestion rather than a fix.
				Range: core.NewTextRange(reported.Pos(), reported.End()+1),
				Text:  "",
			}},
		})
}
