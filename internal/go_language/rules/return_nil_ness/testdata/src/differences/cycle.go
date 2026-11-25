package differences

// CycleOuter and CycleInner call each other, the one place the push's answer hangs on the order it walks the package
// in. Walked in the order declared, CycleOuter begins first, CycleInner's call back into it answers the defaults, so
// CycleInner may return nil and only CycleOuter, which allocates, is marked. Begun at CycleInner, as a reader asking of
// it alone would begin, CycleOuter would be judged whole first and CycleInner would return what it returns, never nil.

// CycleOuter is never nil: whatever CycleInner returns, it returns a fresh node.
func CycleOuter(node *Node) *Node { // want CycleOuter:`nilness: \[\{MaybeNil NeverNil\}\]`
	if node != nil {
		node.Next = CycleInner(node.Next)
	}
	return &Node{}
}

// CycleInner returns what CycleOuter returns, which the push, reaching it from CycleOuter, does not know yet.
func CycleInner(node *Node) *Node {
	return CycleOuter(node)
}
