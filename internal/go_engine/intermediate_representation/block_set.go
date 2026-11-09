// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file carries the two parts of go/ir's lift.go that the builder needs without lifting:
// BlockSet, which Function.blockset hands out, and removeInstr, which edits a referrer list.
// The rest of lift.go comes with lifting (#ee33njz).

func removeInstruction(refs []Instruction, instr Instruction) []Instruction {
	instructions := refs
	i := -1
	for j := range instructions {
		if instr == instructions[j] {
			i = j
			break
		}
	}
	if i == -1 {
		return instructions
	}
	// Don't start copying elements until we find one to delete.
	for j := i + 1; j < len(instructions); j++ {
		if instruction := instructions[j]; instruction != instr {
			instructions[i] = instruction
			i++
		}
	}
	clear(instructions[i:]) // zero/nil out the obsolete elements, for GC
	return instructions[:i]
}

type BlockSet struct {
	index  int
	values []bool
	count  int
}

func NewBlockSet(size int) *BlockSet {
	return &BlockSet{values: make([]bool, size)}
}

func (set *BlockSet) Set(s2 *BlockSet) {
	copy(set.values, s2.values)
	set.count = 0
	for _, value := range set.values {
		if value {
			set.count++
		}
	}
}

func (set *BlockSet) Count() int {
	return set.count
}

func (set *BlockSet) Has(block *BasicBlock) bool {
	if block.Index >= len(set.values) {
		return false
	}
	return set.values[block.Index]
}

// add adds `block` to the set and returns true if the set changed.
func (set *BlockSet) Add(block *BasicBlock) bool {
	if set.values[block.Index] {
		return false
	}
	set.count++
	set.values[block.Index] = true
	set.index = block.Index

	return true
}

func (set *BlockSet) Clear() {
	for index := range set.values {
		set.values[index] = false
	}
	set.count = 0
}

// take removes an arbitrary element from a set s and
// returns its index, or returns -1 if empty.
func (set *BlockSet) Take() int {
	// [i, end]
	for index := set.index; index < len(set.values); index++ {
		if set.values[index] {
			set.values[index] = false
			set.index = index
			set.count--
			return index
		}
	}

	// [start, i)
	for index := 0; index < set.index; index++ {
		if set.values[index] {
			set.values[index] = false
			set.index = index
			set.count--
			return index
		}
	}

	return -1
}
