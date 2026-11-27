package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// forEachFunctionLike calls visit for every function-like node in the tree, outermost first.
//
// Nested functions are visited by `Lower` through the function arena, so this deliberately does not
// descend into a function it has already handed over: doing so would lower each inner function
// twice, once standalone and once as a child, and the standalone copy would have no context.
func forEachFunctionLike(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		forEachFunctionLike(node, visit)
		return false
	})
}
