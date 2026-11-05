package fixture

// Grove has a real method declared beside it, so the file serves Grove.
type Grove struct {
	leaves []*Leaf
}

func (grove *Grove) Size() int {
	return len(grove.leaves)
}
