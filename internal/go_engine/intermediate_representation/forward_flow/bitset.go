package forward_flow

// bitset is a fixed size set of small integers.
type bitset []uint64

func newBitset(size int) bitset {
	return make(bitset, (size+63)/64)
}

// add adds member and reports whether it was not there before.
func (set bitset) add(member int) bool {
	if set.contains(member) {
		return false
	}
	set[member/64] |= 1 << (member % 64)
	return true
}

func (set bitset) remove(member int) {
	set[member/64] &= ^(1 << (member % 64))
}

func (set bitset) contains(member int) bool {
	return set[member/64]&(1<<(member%64)) != 0
}
