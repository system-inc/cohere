package flagged

// One declared type plus a method of another: Leaf is declared in leaf.go, so the finding sits on the receiver of its
// method here.
type Tree struct {
	leaves []Leaf
}

func (tree *Tree) Count() int {
	return len(tree.leaves)
}

func (leaf *Leaf) Fall() { // want "already serves `Tree`, and `Leaf` is a second type with methods"
	leaf.fallen = true
}

// A second method of Leaf in the same file is the same extra type, so it is reported once.
func (leaf *Leaf) Rise() {
	leaf.fallen = false
}
