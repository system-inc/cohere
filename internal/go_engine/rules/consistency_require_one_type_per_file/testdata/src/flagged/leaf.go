package flagged

type Leaf struct {
	fallen bool
}

func (leaf Leaf) Fallen() bool {
	return leaf.fallen
}
