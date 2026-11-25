package intermediate_representation

// BlockSet is a set of one function's blocks, held as one flag per block index. It is the part of go/ir's BlockSet
// (honnef.co/go/tools@v0.8.1 go/ir/lift.go) that FindLoops' loops need, translated, not copied: NewBlockSet, Has and
// Add. go/ir's also keeps a count and the last index added, which only its Set, Num and lifting read, so ours keeps
// neither.
type BlockSet struct {
	values []bool
}

// NewBlockSet returns an empty set with room for blocks indexed below size.
func NewBlockSet(size int) *BlockSet {
	return &BlockSet{values: make([]bool, size)}
}

// Has reports whether block is in the set. A block indexed past the set's size is not.
func (set *BlockSet) Has(block *BasicBlock) bool {
	if block.Index >= len(set.values) {
		return false
	}
	return set.values[block.Index]
}

// Add adds block to the set and reports whether the set changed, that is whether block was not already in it.
func (set *BlockSet) Add(block *BasicBlock) bool {
	if set.values[block.Index] {
		return false
	}
	set.values[block.Index] = true
	return true
}
