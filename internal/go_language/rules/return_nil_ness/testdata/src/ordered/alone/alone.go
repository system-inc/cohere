// Package alone holds a function that calls only itself and says more than the defaults of its signature. It is begun
// whenever its own body is judged, in any order, so it reads itself as the defaults in every walk, and the order bound
// never names it.
package alone

// Node is a link of a list.
type Node struct {
	Next *Node
}

// Chain is never nil: it returns a fresh node, linked to the chain below it.
func Chain(depth int) *Node {
	node := &Node{}
	if depth > 0 {
		node.Next = Chain(depth - 1)
	}
	return node
}
