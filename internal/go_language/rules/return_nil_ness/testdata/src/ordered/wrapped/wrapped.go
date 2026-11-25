// Package wrapped holds a method that says more than the defaults of its signature, read through its bound method
// wrapper by a function declared before it. The push walks Wrapped first and reads Fresh's fact before it has judged
// Fresh, so Wrapped gets the defaults. A question of Wrapped replays the push, and the order bound names Fresh.
package wrapped

// Node is a link of a list.
type Node struct {
	Next *Node
}

// Wrapped returns what Fresh returns, through a method value, whose wrapper, a function of no package, stands for
// Fresh.
func Wrapped(node *Node) *Node {
	fresh := node.Fresh
	return fresh()
}

// Fresh is never nil.
func (node *Node) Fresh() *Node {
	return &Node{}
}
