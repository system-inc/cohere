package tailwind

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// matchedNodes collects what a walk matched: a string literal, or a template literal whose text was
// matched, either as itself or as its runs.
type matchedNodes []*ast.Node

func (matched *matchedNodes) add(node *ast.Node) {
	for _, existing := range *matched {
		if existing == node {
			return
		}
	}
	*matched = append(*matched, node)
}
