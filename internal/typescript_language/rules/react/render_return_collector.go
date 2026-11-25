package react

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// renderReturnCollector gathers the components in source order and the returns that satisfy them.
//
// Source order matters for the report, not for the judgment: two broken components in one file
// report twice and upstream emits them in the order they were declared, which a map would not
// preserve. The index beside the slice is what makes the nested case cheap, since a return marks
// the nearest enclosing entry and that is a walk up the parent chain rather than a search.
type renderReturnCollector struct {
	components []*renderReturnComponent
	byNode     map[*ast.Node]*renderReturnComponent
}

// walk collects every component and then every return, in one pre-order pass.
//
// One pass is enough because a return is always inside the component it marks, so the component
// entry exists by the time the return is reached. Upstream gets the same ordering from ESLint's own
// traversal, where `ClassDeclaration` fires before the returns in its body and `Program:exit` runs
// after everything.
func (collector *renderReturnCollector) walk(node *ast.Node) {
	if node == nil {
		return
	}

	if isRenderReturnComponent(node) {
		component := &renderReturnComponent{
			node:           node,
			renderProperty: findRenderProperty(node),
		}
		collector.components = append(collector.components, component)
		if collector.byNode == nil {
			collector.byNode = map[*ast.Node]*renderReturnComponent{}
		}
		collector.byNode[node] = component
	}

	if node.Kind == ast.KindReturnStatement {
		collector.markEnclosingComponent(node.Parent)
	}

	// An arrow with an expression body returns without writing a `ReturnStatement`, so nothing in
	// the walk above can see it. Upstream carries a second listener for exactly this and it is not
	// a shortcut for the first: `render = () => <div/>` is clean and has no return node anywhere.
	//
	// Upstream's `ArrowFunctionExpression` listener tests only that the arrow has an expression
	// body and that its parent property is named `render`, then marks unconditionally, without
	// consulting the scope count at all. This arm needs no separate flag to reproduce that: the
	// climb starts at the arrow's own parent, which `isRenderReturnImplicitReturnArrow` has
	// already established is the `render` property, so the shared count is zero when the property
	// test runs and the walk satisfies it on the first step every time.
	//
	// That was measured rather than argued. Seeding the flag true for this arm was in the first
	// draft, and a mutation forcing the seed false was a survivor: the seed is subsumed by the
	// property check and no input can separate the two, because the arm's own precondition is what
	// guarantees the property is the first node the climb sees. Removed rather than kept with a
	// fixture, since no fixture could have covered it.
	if node.Kind == ast.KindArrowFunction && isRenderReturnImplicitReturnArrow(node) {
		collector.markEnclosingComponent(node.Parent)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		collector.walk(child)
		return false
	})
}

// markEnclosingComponent reproduces upstream's `components.set`, which is the whole subtlety.
//
// The walk climbs to the nearest registered component and marks it, so a return inside a nested
// class that is not a component marks the outer one. See the rule doc: that is a measured false
// negative in upstream and it is reproduced here rather than corrected.
//
// The scope count is applied on the way up. A return only counts if the property named `render` is
// reached having crossed at most one function-like scope, which is upstream's `depth <= 1`. The
// climb continues past the property either way, because the component still has to be found even
// when the return does not satisfy it, and because upstream's `forEach` over the ancestors likewise
// does not stop at the first match.
func (collector *renderReturnCollector) markEnclosingComponent(from *ast.Node) {
	functionScopeDepth := 0
	satisfiesRender := false

	for current := from; current != nil; current = current.Parent {
		if isRenderReturnFunctionScope(current) {
			functionScopeDepth++
		}
		if isRenderNamedProperty(current) {
			// A class method or accessor is one node here and two upstream, so the function scope
			// it owns is counted at the property rather than by the walk. See the rule doc: this
			// is the same correction the sibling rules make and it does move the verdict here.
			depthAtProperty := functionScopeDepth
			switch current.Kind {
			case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
				depthAtProperty++
			}
			if depthAtProperty <= 1 {
				satisfiesRender = true
			}
		}
		if component, found := collector.byNode[current]; found {
			if satisfiesRender {
				component.hasReturnStatement = true
			}
			return
		}
	}
}
