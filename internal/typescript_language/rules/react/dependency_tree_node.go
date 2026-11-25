package react

import "strings"

// dependencyTreeNode is one segment of a property path, and the three facts the descent reads.
//
// The tree is what makes the prefix asymmetry fall out instead of having to be stated: `props.a.b`
// and `props.a.b.c` share the `props.a.b` node, so marking that node satisfied stops the descent
// before it can reach `.c`, while marking `.c` satisfied leaves `props.a.b` unsatisfied above it.
type dependencyTreeNode struct {
	// isUsed is set when the callback reads exactly this path.
	isUsed bool

	// isSatisfiedRecursively is set when the dependency array declares this path, which covers
	// everything beneath it.
	isSatisfiedRecursively bool

	// isSubtreeUsed is set when the callback reads this path or anything under it. It is what
	// separates a declared dependency that is doing work from one that is unnecessary.
	isSubtreeUsed bool

	children map[string]*dependencyTreeNode
	order    []string
}

// childForPath walks or creates the chain of nodes naming a dotted path.
func (node *dependencyTreeNode) childForPath(path string) *dependencyTreeNode {
	current := node
	for _, segment := range strings.Split(path, ".") {
		child, found := current.children[segment]
		if !found {
			child = newDependencyTreeNode()
			current.children[segment] = child
			current.order = append(current.order, segment)
		}
		current = child
	}
	return current
}

// markAncestorsSubtreeUsed marks every node along a path as having a read somewhere beneath it.
func (node *dependencyTreeNode) markAncestorsSubtreeUsed(path string) {
	current := node
	for _, segment := range strings.Split(path, ".") {
		child, found := current.children[segment]
		if !found {
			return
		}
		child.isSubtreeUsed = true
		current = child
	}
}
