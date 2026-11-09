package forward_flow

import "slices"

// reversePostorder is graph's nodes in reverse postorder of its spanning tree, a topological order when the graph
// has no cycle.
func reversePostorder(graph Graph) []int {
	order := postorder(graph)
	slices.Reverse(order)
	return order
}

// postorder is graph's nodes in postorder of a depth first search that visits every node, in ascending order, so a
// node no root reaches is still ordered and the order is stable. An edge back to a node still on the search's stack is
// skipped, which breaks each cycle there.
func postorder(graph Graph) []int {
	nodeCount := graph.NumNodes()
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

		for successor := range graph.Out(node) {
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
