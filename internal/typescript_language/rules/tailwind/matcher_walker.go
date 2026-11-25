package tailwind

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// matcherWalker is one walk's state. Its one visitor is made once per walk, since a closure per node
// would be an allocation for every node under every class surface in the file.
type matcherWalker struct {
	matchers   []compiledMatcher
	matched    *matchedNodes
	live       uint64
	visitChild func(*ast.Node) bool
}

// visit is upstream's findMatchingNestedNodes for the three matchers that never nest: a node is asked
// by every matcher still live, kept when one matches, and entered by those not stopped at it.
func (walker *matcherWalker) visit(node *ast.Node) bool {
	if node == nil {
		return false
	}
	isMatch, survivors := evaluateMatchers(node, walker.live, walker.matchers)
	if isMatch {
		walker.matched.add(node)
	}
	if survivors == 0 {
		return false
	}
	live := walker.live
	walker.live = survivors
	walker.descend(node)
	walker.live = live
	return false
}

// descend enters a node's children as ESTree has them.
func (walker *matcherWalker) descend(node *ast.Node) {
	switch node.Kind {
	// ESTree's method is a key beside a function expression, and every matcher stops at a function
	// expression, so only the key is entered.
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		walker.visit(node.Name())
		return
	case ast.KindConstructor:
		return

	// A template's runs are asked as quasis, all at once since they share every ancestor, and its holes
	// are entered as any expression is.
	case ast.KindTemplateExpression:
		if isMatch, _ := evaluateQuasi(node, walker.live, walker.matchers); isMatch {
			walker.matched.add(node)
		}
		template := node.AsTemplateExpression()
		if template.TemplateSpans != nil {
			for _, span := range template.TemplateSpans.Nodes {
				walker.visit(span.AsTemplateSpan().Expression)
			}
		}
		return

	// A template with no holes is one quasi in ESTree, a child of the literal.
	case ast.KindNoSubstitutionTemplateLiteral:
		if isMatch, _ := evaluateQuasi(node, walker.live, walker.matchers); isMatch {
			walker.matched.add(node)
		}
		return
	}

	node.ForEachChild(walker.visitChild)
}
