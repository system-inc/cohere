package intermediate_representation

import (
	"math"
)

type sccState struct {
	function *Function
	index    int
	lows     BlockMap[int]
	F        []int
	sccs     []*Scc
}

// scc finds the strongly connected components reachable from block, appending each to state.sccs as it closes.
//
// Mutates: block and every block reached from it gain their strongly connected component
func (state *sccState) scc(block *BasicBlock) {
	// This is Tarjan's algorithm, with some optimizations from [1] applied.
	//
	// [1] R. E. Tarjan and U. Zwick, “Finding strong components using depth-first search,” Apr. 11, 2022, arXiv:
	// arXiv:2201.07197. doi: 10.48550/arXiv.2201.07197.
	vi := block.Index
	lows := state.lows

	state.index += 2
	lows[vi] = state.index

	for _, successor := range block.Successors {
		wi := successor.Index

		if lows[wi] == 0 {
			state.scc(successor)
		}

		if lows[wi] < lows[vi] {
			lows[vi] = lows[wi] | 1
		}
	}

	if lows[vi]&1 == 0 {
		leaderLow := lows[vi]

		scc := &Scc{
			Index: len(state.sccs),
		}
		stack := state.F
		for lows[stack[len(stack)-1]] >= leaderLow {
			x := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			lows[x] = math.MaxInt
			scc.Blocks = append(scc.Blocks, state.function.Blocks[x])
			state.function.Blocks[x].Scc = scc
		}
		state.F = stack

		lows[vi] = math.MaxInt
		scc.Blocks = append(scc.Blocks, block)
		block.Scc = scc
		state.sccs = append(state.sccs, scc)
	} else {
		state.F = append(state.F, vi)
	}
}

// buildSccs computes the strongly connected components of fn's control flow graph.
//
// Mutates: function gains its strongly connected components, and its blocks theirs
func buildSccs(function *Function) {
	blockCount := len(function.Blocks)

	if blockCount == 0 {
		return
	}

	state := sccState{
		function: function,
		index:    0,
		// The +1 makes space for the sentinel value stored in F.
		lows: make(BlockMap[int], blockCount+1),
		F:    []int{blockCount},
	}

	// All blocks are reachable from the entry block.
	state.scc(function.Blocks[0])

	seen := make([]bool, len(state.sccs))
	for _, scc := range state.sccs {
		clear(seen)
		for _, block := range scc.Blocks {
			for _, succ := range block.Successors {
				if succ.Scc != block.Scc && !seen[succ.Scc.Index] {
					block.Scc.Successors = append(block.Scc.Successors, succ.Scc)
					succ.Scc.Predecessors = append(succ.Scc.Predecessors, block.Scc)
					seen[succ.Scc.Index] = true
				}
			}
		}
	}

	function.Sccs = state.sccs

	// This iterates over the SCCs in reverse topological order, i.e., "bottom
	// up". This allows us to build up reachability sets.
	for _, scc := range function.Sccs {
		scc.reachable.SetBit(&scc.reachable, scc.Index, 1)
		for _, succ := range scc.Successors {
			scc.reachable.Or(&scc.reachable, &succ.reachable)
		}
	}
}
