package static_single_assignment

// Dominance is the immediate-dominator array over a function's block slice, by position in it.
type Dominance struct {
	// position maps a block id to its index in the order ComputeDominance made: a reverse postorder of
	// the real edges from the entry, then the blocks they don't reach.
	position map[BlockId]int
	// immediate[i] is the index of block i's immediate dominator; the entry is its own, and a block the
	// real edges don't reach has none (-1), dominated by nothing but itself.
	immediate []int
}

// Dominates reports whether every path from the entry to block passes through dominator.
//
// A block dominates itself, the standard convention.
func (dominance *Dominance) Dominates(dominator, block BlockId) bool {
	target, ok := dominance.position[dominator]
	if !ok {
		return false
	}
	index, ok := dominance.position[block]
	if !ok {
		return false
	}
	for {
		if index == target {
			return true
		}
		if index == 0 || dominance.immediate[index] == -1 {
			return false
		}
		index = dominance.immediate[index]
	}
}
