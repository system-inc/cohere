package intermediate_representation

import (
	"slices"
	"sort"
)

// DominatorPreorder returns a new slice containing the blocks of f
// in a preorder traversal of the dominator tree.
func (function *Function) DominatorPreorder() []*BasicBlock {
	slice := slices.Clone(function.Blocks)
	sort.Slice(slice, func(left, right int) bool {
		return slice[left].dom.pre < slice[right].dom.pre
	})
	return slice
}

// DominatorPostorder returns a new slice containing the blocks of f
// in a postorder traversal of the dominator tree.
// (This is not the same as a postdominance order.)
func (function *Function) DominatorPostorder() []*BasicBlock {
	slice := slices.Clone(function.Blocks)
	sort.Slice(slice, func(left, right int) bool {
		return slice[left].dom.post < slice[right].dom.post
	})
	return slice
}
