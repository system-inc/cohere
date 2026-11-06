package mutation_aliasing

import ssa "github.com/system-inc/cohere/static_single_assignment"

type testFunction struct {
	entry            ssa.BlockId
	blocks           []*testBlock
	table            map[ssa.BlockId]*testBlock
	next             ssa.BlockId
	params           []testPlace
	context          []testPlace
	returns          *testPlace
	parametersFrozen bool
}

func (function *testFunction) block() *testBlock {
	block := &testBlock{id: function.next}
	function.next++
	function.blocks = append(function.blocks, block)
	function.table[block.id] = block
	if function.entry == 0 {
		function.entry = block.id
	}
	return block
}
