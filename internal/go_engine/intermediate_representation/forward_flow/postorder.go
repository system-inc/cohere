package forward_flow

import (
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// reversePostorder is function's blocks in reverse postorder of its spanning tree, a topological order when the graph
// has no cycle.
func reversePostorder(function *intermediate_representation.Function) []int {
	order := postorder(function)
	slices.Reverse(order)
	return order
}

// postorder is function's blocks in postorder of a depth first search that visits every block, in ascending order of
// index, so a block no root reaches is still ordered and the order is stable. An edge back to a block still on the
// search's stack is skipped, which breaks each cycle there.
func postorder(function *intermediate_representation.Function) []int {
	nodeCount := function.NumNodes()
	if nodeCount == 0 {
		return nil
	}

	order := make([]int, 0, nodeCount)
	visited := newBitset(nodeCount)
	onStack := newBitset(nodeCount)

	var visit func(node int)
	visit = func(node int) {
		if !visited.add(node) {
			return
		}
		onStack.add(node)

		for successor := range function.Out(node) {
			if onStack.contains(successor) {
				continue
			}
			visit(successor)
		}

		onStack.remove(node)
		order = append(order, node)
	}

	for node := range nodeCount {
		visit(node)
	}

	return order
}
