// Package differences holds the loop where the port and staticcheck part: two values carried around a loop, one set
// from the other, so the phi of the one reads the phi of the other through the same edge. Read as a phi means, every
// edge before any phi is set, the dependent value is the other's value from before the edge, which the loop
// dereferenced, so it is never nil whichever variable is declared first. Staticcheck sets the phis one after another,
// in the order lift.go places them, and so reads the other's new value when its phi stands first.
package differences

// Node is a value results point at.
type Node struct {
	Kind int
	Next *Node
}

// The dependent outer is declared before inner.
func OuterFirst(node *Node) *Node { // want OuterFirst:`nilness: \[\{MaybeNil NeverNil\}\]`
	outer := node
	inner := node.Next
	for inner.Kind == 1 {
		outer = inner
		inner = outer.Next
	}
	return outer
}

// The dependent outer is declared after inner.
func InnerFirst(node *Node) *Node { // want InnerFirst:`nilness: \[\{MaybeNil NeverNil\}\]`
	inner := node.Next
	outer := node
	for inner.Kind == 1 {
		outer = inner
		inner = outer.Next
	}
	return outer
}
