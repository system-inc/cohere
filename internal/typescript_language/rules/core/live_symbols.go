package core

// liveSymbols is the set of a root's symbols live at one point: bit n stands for the symbol its
// events number n (deadStoreEvent.number). Nil is the empty set, and a set reads as zero past its
// end, so sets of different lengths compare and combine as the sets they stand for.
//
// A bitset rather than the map it replaced, because a root's symbols are few and fixed once its
// graph is built: one or two words, against a map per block per round (#tmn9n27).
type liveSymbols []uint64

// has reports whether symbol number is in the set.
func (set liveSymbols) has(number int) bool {
	word := number / 64
	return word < len(set) && set[word]&(1<<(number%64)) != 0
}

// covers reports whether every symbol in other is also in set.
func (set liveSymbols) covers(other liveSymbols) bool {
	for index, bits := range other {
		var mine uint64
		if index < len(set) {
			mine = set[index]
		}
		if bits&^mine != 0 {
			return false
		}
	}
	return true
}

// withWords returns a copy of set, words long, which the caller may write without touching set.
func (set liveSymbols) withWords(words int) liveSymbols {
	copied := make(liveSymbols, words)
	copy(copied, set)
	return copied
}
