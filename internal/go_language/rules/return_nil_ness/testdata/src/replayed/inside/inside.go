// Package inside holds a cycle whose nilness hangs on the order the push walks the package in, so a question of Inner
// replays the push's walk from Outer, the first root. Inside that replay, while Outer and Inner are both being judged,
// Inner's call to outside.Value asks of a restored package with no answer, which ends the question in a panic.
package inside

import "replayed/outside"

// Node is a link of a list.
type Node struct {
	Next *Node
}

// Outer is never nil: whatever Inner returns, it returns a fresh node.
func Outer(node *Node) *Node {
	if node != nil {
		node.Next = Inner(node.Next)
	}
	return &Node{}
}

// Inner calls back into Outer first, which makes the cycle, and then asks outside.Value.
func Inner(node *Node) *Node {
	next := Outer(node)
	if outside.Value() == nil {
		return nil
	}
	return next
}
