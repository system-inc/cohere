package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// functionNameMatchingChecker carries the resolved configuration across the listeners.
type functionNameMatchingChecker struct {
	ruleContext                rule.Context
	direction                  string
	considerPropertyDescriptor bool
	includeModuleExports       bool
}

// shouldWarn answers upstream's `shouldWarn`: the direction decides which way the comparison runs.
func (checker *functionNameMatchingChecker) shouldWarn(left string, right string) bool {
	if checker.direction == "never" {
		return left == right
	}
	return left != right
}

// report emits the finding, choosing among the four messages the way upstream's `report` does.
func (checker *functionNameMatchingChecker) report(node *ast.Node, name string, functionName string, isProperty bool) {
	checker.ruleContext.ReportNode(node, functionNameMatchingMessage(checker.direction, isProperty, functionName, name))
}

// checkVariableDeclarator is upstream's `VariableDeclarator` listener.
//
// Our parser groups declarators under one `KindVariableDeclaration` per `var`, so the listener fires
// once and iterates rather than firing per declarator. Upstream reports on the declarator, so the
// finding is anchored on each declaration individually rather than on the statement.
func (checker *functionNameMatchingChecker) checkVariableDeclarator(node *ast.Node) {
	declaration := node.AsVariableDeclaration()
	if declaration == nil || declaration.Initializer == nil {
		return
	}
	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return
	}
	functionName, named := functionNameMatchingFunctionExpressionName(declaration.Initializer)
	if !named {
		return
	}
	if checker.shouldWarn(name.Text(), functionName) {
		checker.report(node, name.Text(), functionName, false)
	}
}

// checkAssignment is upstream's `AssignmentExpression` listener.
func (checker *functionNameMatchingChecker) checkAssignment(node *ast.Node) {
	expression := node.AsBinaryExpression()
	if expression == nil || expression.OperatorToken == nil ||
		!functionNameMatchingIsAssignmentOperator(expression.OperatorToken.Kind) {
		return
	}
	functionName, named := functionNameMatchingFunctionExpressionName(expression.Right)
	if !named {
		return
	}

	target := expression.Left
	switch target.Kind {
	case ast.KindIdentifier:
		if checker.shouldWarn(target.Text(), functionName) {
			checker.report(node, target.Text(), functionName, false)
		}

	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		// `module.exports = function foo(){}` is exempt unless the option turns it on. Checked
		// before the name is read, matching upstream's guard order.
		if !checker.includeModuleExports && functionNameMatchingIsModuleExports(target) {
			return
		}
		name, readable := functionNameMatchingStaticPropertyName(target)
		if !readable || !functionNameMatchingIsIdentifier(name) {
			return
		}
		if checker.shouldWarn(name, functionName) {
			checker.report(node, name, functionName, true)
		}
	}
}

// checkProperty is upstream's `"Property, PropertyDefinition[value]"` listener.
func (checker *functionNameMatchingChecker) checkProperty(node *ast.Node) {
	value := functionNameMatchingPropertyValue(node)
	if value == nil {
		return
	}
	functionName, named := functionNameMatchingFunctionExpressionName(value)
	if !named {
		return
	}

	key := node.Name()
	if key == nil {
		return
	}

	// A computed key whose expression is a string literal is still a static name upstream:
	// `({['foo']: function bar(){}})` reports. Any other computed key -- `['b' + 'ar']`, or a
	// variable -- is not comparable and is declined. Unwrapping here rather than in the literal
	// arm keeps the identifier arm below reading only genuine identifiers.
	if key.Kind == ast.KindComputedPropertyName {
		computed := key.AsComputedPropertyName()
		if computed == nil || computed.Expression == nil ||
			computed.Expression.Kind != ast.KindStringLiteral {
			return
		}
		keyText := computed.Expression.Text()
		if !functionNameMatchingIsIdentifier(keyText) {
			return
		}
		if checker.shouldWarn(keyText, functionName) {
			checker.report(node, keyText, functionName, true)
		}
		return
	}

	if key.Kind == ast.KindIdentifier {
		propertyName := key.Text()

		// The property-descriptor arm. `{value: function foo(){}}` inside `Object.defineProperty`
		// compares against the property being defined rather than against the literal key `value`.
		if checker.considerPropertyDescriptor && propertyName == "value" &&
			node.Parent != nil && node.Parent.Kind == ast.KindObjectLiteralExpression {
			descriptorName, isDescriptor, readable := functionNameMatchingDescriptorTarget(node.Parent)
			if isDescriptor {
				// Inside a descriptor whose target name cannot be read -- `'b' + 'ar'`, or a
				// computed key -- upstream reports nothing. It does not fall back to comparing
				// against the literal key `value`, which is what a reader assumes and what an
				// earlier draft here did: each of its branches guards the report with
				// `isStringLiteral(property)` or `!node.parent.parent.computed` and simply
				// declines otherwise. Four of upstream's clean cases pin this.
				if !readable {
					return
				}
				if checker.shouldWarn(descriptorName, functionName) {
					checker.report(node, descriptorName, functionName, true)
				}
				return
			}
		}

		if checker.shouldWarn(propertyName, functionName) {
			checker.report(node, propertyName, functionName, true)
		}
		return
	}

	// A string-literal key is compared only when it spells a legal identifier, so `{'foo-bar':
	// function foo(){}}` is silent: renaming the function to match would not be writable as a name.
	if key.Kind == ast.KindStringLiteral {
		keyText := key.Text()
		if !functionNameMatchingIsIdentifier(keyText) {
			return
		}
		if checker.shouldWarn(keyText, functionName) {
			checker.report(node, keyText, functionName, true)
		}
	}
}
