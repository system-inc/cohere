package react

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noUnusedClassComponentMethodsState is one component's gathered definitions and uses.
type noUnusedClassComponentMethodsState struct {
	// definitions holds the key node of each declaration, which is also the reported span.
	definitions []*ast.Node
	used        map[string]bool
}

// collect walks one member's body recording uses and assignment definitions.
//
// A nested class is not skipped, which matches upstream: its `classInfo` is replaced when the inner
// class declaration is entered and never restored, so the outer class stops collecting at that
// point. That is a defect rather than a design, and reproducing it exactly would mean modelling a
// single mutable slot; measured, the observable difference is that an inner component's members are
// judged and the outer class's remaining members are not. This walk judges each component
// separately and therefore judges more than upstream on a class containing a nested component.
//
// The divergence is stated rather than hidden because it is the one place this port is deliberately
// wider. A class declaring a component inside one of its methods is rare, and the alternative is
// reproducing a bug that silently drops findings. `TestNoUnusedClassComponentMethodsNestedClass`
// records both verdicts.
func (state *noUnusedClassComponentMethodsState) collect(node *ast.Node) {
	if node == nil {
		return
	}

	if node.Kind == ast.KindPropertyAccessExpression || node.Kind == ast.KindElementAccessExpression {
		if object, property := noUnusedClassComponentMethodsAccessParts(node); isThisExpression(object) {
			if name, ok := noUnusedClassComponentMethodsStaticName(node, property); ok {
				if noUnusedClassComponentMethodsIsAssignmentTarget(node) {
					state.definitions = append(state.definitions, property)
				} else {
					state.used[name] = true
				}
			}
		}
	}

	if node.Kind == ast.KindVariableDeclaration {
		declaration := node.AsVariableDeclaration()
		if isThisExpression(declaration.Initializer) && declaration.Name() != nil &&
			declaration.Name().Kind == ast.KindObjectBindingPattern {
			for _, element := range declaration.Name().AsBindingPattern().Elements.Nodes {
				if element.Kind != ast.KindBindingElement {
					continue
				}
				binding := element.AsBindingElement()
				key := binding.PropertyName
				if key == nil {
					key = binding.Name()
				}
				if name, ok := noUnusedClassComponentMethodsLiteralName(key); ok {
					state.used[name] = true
				}
			}
		}
	}

	node.ForEachChild(func(child *ast.Node) bool {
		state.collect(child)
		return false
	})
}

// report emits one finding per definition whose name nothing used.
func (state *noUnusedClassComponentMethodsState) report(
	ruleContext rule.Context,
	className string,
	lifecycle map[string]bool,
) {
	for _, key := range state.definitions {
		name, ok := noUnusedClassComponentMethodsLiteralName(key)
		if !ok {
			continue
		}
		if state.used[name] || noUnusedClassComponentMethodsLifecycle[name] || lifecycle[name] {
			continue
		}

		if className == "" {
			ruleContext.ReportNode(key, rule.Message{
				Id:          "unused",
				Description: noUnusedClassComponentMethodsDescription(name, ""),
			})
			continue
		}
		ruleContext.ReportNode(key, rule.Message{
			Id:          "unusedWithClass",
			Description: noUnusedClassComponentMethodsDescription(name, className),
		})
	}
}
