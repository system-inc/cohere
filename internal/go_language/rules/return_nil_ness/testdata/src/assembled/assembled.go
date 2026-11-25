// Package assembled holds a function declared without a body that only a replay of the push's walk meets, as a root
// before the one asked of, and a function none of whose results may be nil, which both walks meet (#kemy8sq).
// Assembled's stub.s makes the package one go build takes; cohere never assembles it.
package assembled

// Node is a value results point at.
type Node struct {
	Length int
	Next   *Node
}

// Length has no result that may be nil, so its nilness is the defaults of its signature from its types alone, the
// same in every build, and neither walk keeps it.
func Length(node *Node) int {
	return node.Length
}

// Assembled has no body, so its result is the defaults of its signature, unjudged, and no fact. Nothing calls it, so
// only the replay, walking the roots before Inner, meets it. In a build where it had a body, as a function of
// assembly on one platform and of Go on another, it could answer otherwise, so the replay keeps what it answers here.
func Assembled() *Node

// Outer and Inner call each other, so asking of Inner depends on the order the push walks the package in, and the
// resolver replays the push from the top until Inner is judged. Outer always returns a fresh node.
func Outer(node *Node) *Node {
	if node != nil {
		node.Next = Inner(node.Next)
	}
	return &Node{}
}

// Inner returns what Outer returns, which the push, reaching it from Outer, does not know yet, so it may return nil.
func Inner(node *Node) *Node {
	return Outer(node)
}
