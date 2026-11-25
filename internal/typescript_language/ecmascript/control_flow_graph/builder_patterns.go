package control_flow_graph

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// patternReads emits what an identifier or member assignment target evaluates
// before the assigned expression: the receiver and computed key of a member
// target, and the throwable fork ESLint records for naming the target inside a
// `try` block. Destructuring targets are laid out by patternBind instead.
func (builder *Builder[E]) patternReads(node *ast.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.KindIdentifier:
		// A compound-assignment or update-expression target reads the
		// variable before writing it; a plain assignment target does not,
		// but ESLint still counts naming it as a point where the enclosing
		// `try` block can throw, so the handler sees the value the target
		// had before the assignment.
		builder.read(node)
		if isThrowableIdentifier(node) {
			builder.firstThrowableFork()
		}

	case ast.KindParenthesizedExpression:
		builder.patternReads(node.AsParenthesizedExpression().Expression)

	case ast.KindNonNullExpression:
		builder.patternReads(node.AsNonNullExpression().Expression)

	case ast.KindAsExpression:
		builder.patternReads(node.AsAsExpression().Expression)

	case ast.KindSatisfiesExpression:
		builder.patternReads(node.AsSatisfiesExpression().Expression)

	case ast.KindTypeAssertionExpression:
		builder.patternReads(node.AsTypeAssertion().Expression)

	default:
		// A member target such as `obj.x` or `obj[k]` evaluates its receiver.
		builder.expression(node)
	}
}

// patternWrites emits the write of an identifier assignment target after its
// assigned expression. Member targets write no variable.
func (builder *Builder[E]) patternWrites(node *ast.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.KindIdentifier:
		builder.write(node)

	case ast.KindParenthesizedExpression:
		builder.patternWrites(node.AsParenthesizedExpression().Expression)

	case ast.KindNonNullExpression:
		builder.patternWrites(node.AsNonNullExpression().Expression)

	case ast.KindAsExpression:
		builder.patternWrites(node.AsAsExpression().Expression)

	case ast.KindSatisfiesExpression:
		builder.patternWrites(node.AsSatisfiesExpression().Expression)

	case ast.KindTypeAssertionExpression:
		builder.patternWrites(node.AsTypeAssertion().Expression)
	}
}

// patternBind lays out a destructuring target the way it evaluates at run
// time, after the assigned expression has produced its value: each element in
// source order evaluates its computed key, forks around its default, and
// writes its target.
func (builder *Builder[E]) patternBind(node *ast.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case ast.KindIdentifier:
		builder.read(node)
		if isThrowableIdentifier(node) {
			builder.firstThrowableFork()
		}
		builder.write(node)

	case ast.KindOmittedExpression:
		// An array hole binds nothing.

	case ast.KindParenthesizedExpression:
		builder.patternBind(node.AsParenthesizedExpression().Expression)

	case ast.KindNonNullExpression:
		builder.patternBind(node.AsNonNullExpression().Expression)

	case ast.KindAsExpression:
		builder.patternBind(node.AsAsExpression().Expression)

	case ast.KindSatisfiesExpression:
		builder.patternBind(node.AsSatisfiesExpression().Expression)

	case ast.KindTypeAssertionExpression:
		builder.patternBind(node.AsTypeAssertion().Expression)

	case ast.KindObjectBindingPattern, ast.KindArrayBindingPattern:
		pattern := node.AsBindingPattern()
		if pattern.Elements == nil {
			return
		}
		for _, element := range pattern.Elements.Nodes {
			binding := element.AsBindingElement()
			if binding == nil {
				continue
			}
			if binding.PropertyName != nil && binding.PropertyName.Kind == ast.KindComputedPropertyName {
				builder.expression(binding.PropertyName.AsComputedPropertyName().Expression)
			}
			builder.bindWithDefault(binding.Name(), binding.Initializer)
		}

	case ast.KindObjectLiteralExpression:
		for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
			switch property.Kind {
			case ast.KindPropertyAssignment:
				assignment := property.AsPropertyAssignment()
				if name := assignment.Name(); name != nil && name.Kind == ast.KindComputedPropertyName {
					builder.expression(name.AsComputedPropertyName().Expression)
				}
				builder.patternBind(assignment.Initializer)
			case ast.KindShorthandPropertyAssignment:
				shorthand := property.AsShorthandPropertyAssignment()
				builder.bindWithDefault(shorthand.Name(), shorthand.ObjectAssignmentInitializer)
			case ast.KindSpreadAssignment:
				builder.patternBind(property.AsSpreadAssignment().Expression)
			}
		}

	case ast.KindArrayLiteralExpression:
		for _, element := range node.AsArrayLiteralExpression().Elements.Nodes {
			builder.patternBind(element)
		}

	case ast.KindSpreadElement:
		builder.patternBind(node.AsSpreadElement().Expression)

	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		if binary.OperatorToken.Kind == ast.KindEqualsToken {
			builder.bindWithDefault(binary.Left, binary.Right)
			return
		}
		builder.expression(node)

	default:
		// A member target such as `obj.x` or `obj[k]` evaluates its receiver.
		builder.expression(node)
	}
}

// bindWithDefault lays out one pattern element: a fork around the default
// value, which only runs when the incoming value is undefined, then the
// target's own binding.
func (builder *Builder[E]) bindWithDefault(target *ast.Node, fallback *ast.Node) {
	if fallback != nil {
		join := builder.newBlock()
		builder.link(builder.current, join)
		withFallback := builder.newBlock()
		builder.link(builder.current, withFallback)

		builder.enter(withFallback)
		builder.expression(fallback)
		builder.link(builder.current, join)

		builder.enter(join)
	}
	builder.patternBind(target)
}

// isDestructuringTarget reports whether an assignment's left-hand side is an
// object or array pattern.
func isDestructuringTarget(node *ast.Node) bool {
	unwrapped := ast.SkipParentheses(node)
	return unwrapped != nil &&
		(unwrapped.Kind == ast.KindObjectLiteralExpression || unwrapped.Kind == ast.KindArrayLiteralExpression)
}
