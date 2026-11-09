// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file carries the two parts of go/ir's lift.go that the builder needs without lifting:
// BlockSet, which Function.blockset hands out, and removeInstr, which edits a referrer list.
// The rest of lift.go comes with lifting (#ee33njz).

func removeInstruction(refs []Instruction, instr Instruction) []Instruction {
	s := refs
	i := -1
	for j := range s {
		if instr == s[j] {
			i = j
			break
		}
	}
	if i == -1 {
		return s
	}
	// Don't start copying elements until we find one to delete.
	for j := i + 1; j < len(s); j++ {
		if v := s[j]; v != instr {
			s[i] = v
			i++
		}
	}
	clear(s[i:]) // zero/nil out the obsolete elements, for GC
	return s[:i]
}

type BlockSet struct {
	idx    int
	values []bool
	count  int
}

func NewBlockSet(size int) *BlockSet {
	return &BlockSet{values: make([]bool, size)}
}

func (s *BlockSet) Set(s2 *BlockSet) {
	copy(s.values, s2.values)
	s.count = 0
	for _, v := range s.values {
		if v {
			s.count++
		}
	}
}

func (s *BlockSet) Num() int {
	return s.count
}

func (s *BlockSet) Has(b *BasicBlock) bool {
	if b.Index >= len(s.values) {
		return false
	}
	return s.values[b.Index]
}

// add adds b to the set and returns true if the set changed.
func (s *BlockSet) Add(b *BasicBlock) bool {
	if s.values[b.Index] {
		return false
	}
	s.count++
	s.values[b.Index] = true
	s.idx = b.Index

	return true
}

func (s *BlockSet) Clear() {
	for j := range s.values {
		s.values[j] = false
	}
	s.count = 0
}

// take removes an arbitrary element from a set s and
// returns its index, or returns -1 if empty.
func (s *BlockSet) Take() int {
	// [i, end]
	for i := s.idx; i < len(s.values); i++ {
		if s.values[i] {
			s.values[i] = false
			s.idx = i
			s.count--
			return i
		}
	}

	// [start, i)
	for i := 0; i < s.idx; i++ {
		if s.values[i] {
			s.values[i] = false
			s.idx = i
			s.count--
			return i
		}
	}

	return -1
}
