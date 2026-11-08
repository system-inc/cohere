package project

import (
	"math/bits"
	"slices"
)

// coverage is a set of the uncompiled files, by index.
type coverage struct {
	size  int
	words []uint64
}

func newCoverage(size int) coverage {
	return coverage{size: size, words: make([]uint64, (size+63)/64)}
}

// set adds index, into the words the coverage shares with every copy of it.
func (set coverage) set(index int) {
	set.words[index/64] |= 1 << (index % 64)
}

// add adds every index of other, into the words the coverage shares with every copy of it.
func (set coverage) add(other coverage) {
	for index := range set.words {
		set.words[index] |= other.words[index]
	}
}

func (set coverage) has(index int) bool {
	return set.words[index/64]&(1<<(index%64)) != 0
}

func (set coverage) with(other coverage) coverage {
	union := newCoverage(set.size)
	for index := range set.words {
		union.words[index] = set.words[index] | other.words[index]
	}
	return union
}

func (set coverage) holds(other coverage) bool {
	for index := range set.words {
		if other.words[index]&^set.words[index] != 0 {
			return false
		}
	}
	return true
}

func (set coverage) equal(other coverage) bool {
	return slices.Equal(set.words, other.words)
}

func (set coverage) empty() bool {
	return set.count() == 0
}

func (set coverage) count() int {
	total := 0
	for _, word := range set.words {
		total += bits.OnesCount64(word)
	}
	return total
}
