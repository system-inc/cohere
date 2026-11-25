package flagged

// The first type in source order is the one the file serves: Branch's method comes before Root's declaration, so Root
// and Wheel are the extras, each reported once at its own place.
func (branch Branch) Thickness() int {
	return branch.rings
}

func (root *Root) Depth() int {
	return root.depth
}

type Root struct { // want "already serves `Branch`, and `Root` is a second type with methods"
	depth int
}

// An alias receiver is followed to the type it names, so this method is Wheel's.
func (rim Rim) Spokes() int { // want "already serves `Branch`, and `Wheel` is a second type with methods"
	return rim.spokes
}

type Rim = Wheel
