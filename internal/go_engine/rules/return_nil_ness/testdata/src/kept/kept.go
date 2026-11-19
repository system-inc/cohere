// Package kept holds a caller whose judgment judges four callees of its own package, none of which a reader asks of:
// the resolver must hold an answer of its own for each all the same, as it does for the caller (#kemy8sq). Assembled is
// declared without a body, as a function of assembly is, whose stub.s makes the package one go build takes; cohere
// never assembles it. hidden is unexported, so it has no objectpath, and the resolver holds its answer with no record
// of it for the store.
package kept

// Node is a value results point at.
type Node struct {
	Next *Node
}

// Fresh always returns a fresh node, so its result is never nil, which a fact holds.
func Fresh() *Node {
	return &Node{}
}

// Following returns what its parameter holds, which may be nil: judged, it says no more than the defaults of its
// signature, so no fact.
func Following(node *Node) *Node {
	return node.Next
}

// hidden always returns a fresh node, as Fresh does, so a fact holds its result never nil, but no other package can
// name it.
func hidden() *Node {
	return &Node{}
}

// Assembled has no body to judge, so its result is the defaults of its signature, unjudged, and no fact.
func Assembled() *Node

// Caller calls each of the four directly, so judging it judges them, and returns what one of them returns, which may
// be nil.
func Caller(node *Node) *Node {
	if node == nil {
		return Fresh()
	}
	if node.Next == nil {
		return Assembled()
	}
	if node.Next.Next == nil {
		return hidden()
	}
	return Following(node)
}
