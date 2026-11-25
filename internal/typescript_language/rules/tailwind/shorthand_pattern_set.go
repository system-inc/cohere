package tailwind

// shorthandPatternSet is a set of pattern indexes, one bit each.
type shorthandPatternSet [2]uint64

func (set *shorthandPatternSet) add(pattern int) {
	set[pattern/64] |= 1 << (pattern % 64)
}

// holds reports whether every pattern in needed is in this set.
func (set shorthandPatternSet) holds(needed shorthandPatternSet) bool {
	return set[0]&needed[0] == needed[0] && set[1]&needed[1] == needed[1]
}
