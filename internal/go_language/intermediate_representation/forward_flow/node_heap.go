package forward_flow

import "container/heap"

// nodeHeap is a heap of nodes ordered by reverse postorder, the order in which a forward analysis converges soonest.
type nodeHeap struct {
	// heap is the nodes remaining in the current sweep.
	heap []int
	// deferred is the nodes of the next sweep.
	deferred []int
	// inQueue is a bitmap over the nodes, set for each node waiting in heap or deferred.
	inQueue []int64
	// priority is each node's place in reverse postorder.
	priority []int
	// currentPriority is the priority of the node last dequeued, or -1 at the start of a sweep.
	currentPriority int
}

// initialize prepares the heap for graph's nodes, each prioritized by its place in reverse postorder.
func (nodes *nodeHeap) initialize(graph Graph) {
	nodeCount := graph.NumNodes()
	*nodes = nodeHeap{
		inQueue:         make([]int64, (nodeCount+63)/64),
		priority:        make([]int, nodeCount),
		currentPriority: -1,
	}
	for place, node := range reversePostorder(graph) {
		nodes.priority[node] = place
	}
}

// enqueue adds node unless it is waiting already, to this sweep, or to the next when this sweep has passed it.
func (nodes *nodeHeap) enqueue(node int) {
	if nodes.inQueue[node/64]&(1<<(node%64)) != 0 {
		return
	}
	nodes.inQueue[node/64] |= 1 << (node % 64)

	if nodes.currentPriority >= 0 && nodes.priority[node] <= nodes.currentPriority {
		// A retreating edge, a self edge, or another update to a node this sweep has passed already is coalesced into
		// the next sweep.
		nodes.deferred = append(nodes.deferred, node)
	} else {
		heap.Push(nodes, node)
	}
}

// dequeue removes and returns the next node, starting the next sweep when this one is done.
func (nodes *nodeHeap) dequeue() int {
	if len(nodes.heap) == 0 {
		nodes.heap, nodes.deferred = nodes.deferred, nodes.heap[:0]
		nodes.currentPriority = -1
		heap.Init(nodes)
	}

	node := nodes.heap[0]
	heap.Pop(nodes)
	nodes.inQueue[node/64] &^= 1 << (node % 64)
	nodes.currentPriority = nodes.priority[node]
	return node
}

// pending is whether any node waits, in this sweep or the next.
func (nodes *nodeHeap) pending() bool { return len(nodes.heap) != 0 || len(nodes.deferred) != 0 }

// Len, Less, Swap, Push and Pop make nodeHeap a container/heap of the current sweep's nodes.
func (nodes *nodeHeap) Len() int { return len(nodes.heap) }
func (nodes *nodeHeap) Less(left int, right int) bool {
	return nodes.priority[nodes.heap[left]] < nodes.priority[nodes.heap[right]]
}
func (nodes *nodeHeap) Swap(left int, right int) {
	nodes.heap[left], nodes.heap[right] = nodes.heap[right], nodes.heap[left]
}
func (nodes *nodeHeap) Push(node any) {
	pushed, isNode := node.(int)
	if !isNode {
		// Invariant: enqueue is the only caller of heap.Push, and it pushes a node's index.
		panic("forward_flow: a node pushed onto the heap is no index")
	}
	nodes.heap = append(nodes.heap, pushed)
}
func (nodes *nodeHeap) Pop() any {
	last := len(nodes.heap) - 1
	node := nodes.heap[last]
	nodes.heap = nodes.heap[:last]
	return node
}
