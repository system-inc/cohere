package load

// packageSet is a set of a listing's packages, a bit for each at its place in the listing.
type packageSet []uint64

// add puts the package at place in the set.
func (set packageSet) add(place int) {
	set[place/64] |= 1 << (place % 64)
}

// addAll puts every package of other in the set.
func (set packageSet) addAll(other packageSet) {
	for word := range other {
		set[word] |= other[word]
	}
}

// has reports whether the package at place is in the set.
func (set packageSet) has(place int) bool {
	return set[place/64]&(1<<(place%64)) != 0
}
