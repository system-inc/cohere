// Package floored holds a cycle of two whose members both end at their floor, no more than the defaults of their
// signatures, whichever one the push enters first: each read of the other answers the defaults, finished or not, so
// the order bound names neither.
package floored

// Node is a link of a list.
type Node struct {
	Next *Node
}

// Ping returns nil at the end of the list, or what Pong returns, which may be nil too.
func Ping(node *Node) *Node {
	if node == nil {
		return nil
	}
	return Pong(node.Next)
}

// Pong returns what Ping returns, which may be nil.
func Pong(node *Node) *Node {
	return Ping(node)
}
