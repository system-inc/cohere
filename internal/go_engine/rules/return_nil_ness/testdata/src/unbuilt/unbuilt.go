// Package unbuilt holds a question the resolver answers by replaying the push's walk, and what the replay must leave
// unbuilt (#efs50c3): a function none of whose results may be nil, whose nilness is the defaults of its signature
// whatever its body, and every root past the one asked of.
package unbuilt

// Node is a value results point at.
type Node struct {
	Length int
	Next   *Node
}

// Size has no result that may be nil and nothing calls it, so only the replay, walking the roots before Inner, meets
// it, and answers it unbuilt.
func Size(node *Node) int {
	return node.Length
}

// Count has no result that may be nil. Outer calls it, but a judgment asks a callee's nilness only for a result that
// may be nil, so judging Outer never builds it; the replay meets it as a root before Inner, and answers it unbuilt.
func Count(node *Node) int {
	count := 0
	for ; node != nil; node = node.Next {
		count++
	}
	return count
}

// Outer and Inner call each other, so asking of Inner depends on the order the push walks the package in, and the
// resolver replays the push from the top until Inner is judged. Outer always returns a fresh node.
func Outer(node *Node) *Node {
	if node != nil {
		node.Length = Count(node)
		node.Next = Inner(node.Next)
	}
	return &Node{}
}

// Inner returns what Outer returns, which the push, reaching it from Outer, does not know yet, so it may return nil.
func Inner(node *Node) *Node {
	return Outer(node)
}

// Wrapped returns what a closure returns. The closure always allocates, but neither the push nor the resolver judges
// a closure, which is no root of the walk, so Wrapped may return nil, and a replay stopping at Inner never builds it.
func Wrapped() *Node {
	fresh := func() *Node {
		return &Node{}
	}
	return fresh()
}

// After is declared after Inner, so a replay stopping at Inner never reaches it, nor builds it.
func After() *Node {
	return &Node{}
}
