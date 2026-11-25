package tailwind

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// nestingWalker is upstream's findMatchingNestedNodes where matchers can make matchers.
type nestingWalker struct {
	matched *matchedNodes
}

// visit asks one node. Upstream asks it until no matcher makes another: the matchers made are asked of
// the same node in place of every matcher that was there, and only the last round's survivors go below.
func (walker *nestingWalker) visit(node esNode, live []liveMatcher) {
	if node.node == nil {
		return
	}
	isMatch := false
	current := live
	for len(current) > 0 {
		var next []liveMatcher
		var nested []liveMatcher
		for _, matcher := range current {
			result, made := matcher.evaluate(node)
			switch result {
			case matcherMatch:
				isMatch = true
				next = append(next, matcher)
			case matcherNoMatch:
				next = append(next, matcher)
			case matcherNested:
				nested = append(nested, made...)
			}
		}
		if len(nested) > 0 {
			current = nested
			continue
		}
		current = next
		break
	}
	if isMatch {
		walker.matched.add(node.node)
	}
	if len(current) > 0 {
		walker.descend(node, current)
	}
}

// descend enters a node's children as ESTree has them.
func (walker *nestingWalker) descend(node esNode, live []liveMatcher) {
	if node.asQuasi {
		return
	}
	if node.asFunction {
		for _, parameter := range node.node.Parameters() {
			walker.visit(esNode{node: parameter}, live)
		}
		walker.visit(esNode{node: node.node.Type()}, live)
		walker.visit(esNode{node: node.node.Body()}, live)
		return
	}
	switch node.node.Kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		walker.visit(esNode{node: node.node.Name()}, live)
		walker.visit(esNode{node: node.node, asFunction: true}, live)
		return
	case ast.KindConstructor:
		walker.visit(esNode{node: node.node, asFunction: true}, live)
		return
	case ast.KindTemplateExpression:
		walker.visit(esNode{node: node.node, asQuasi: true}, live)
		if spans := node.node.AsTemplateExpression().TemplateSpans; spans != nil {
			for _, span := range spans.Nodes {
				walker.visit(esNode{node: span.AsTemplateSpan().Expression}, live)
			}
		}
		return
	case ast.KindNoSubstitutionTemplateLiteral:
		walker.visit(esNode{node: node.node, asQuasi: true}, live)
		return
	}
	node.node.ForEachChild(func(child *ast.Node) bool {
		walker.visit(esNode{node: child}, live)
		return false
	})
}
