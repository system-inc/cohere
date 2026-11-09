package intermediate_representation

import (
	"math"
)

type sccState struct {
	fn    *Function
	index int
	lows  BlockMap[int]
	F     []int
	sccs  []*Scc
}

func (s *sccState) scc(v *BasicBlock) {
	// This is Tarjan's algorithm, with some optimizations from [1] applied.
	//
	// [1] R. E. Tarjan and U. Zwick, “Finding strong components using depth-first search,” Apr. 11, 2022, arXiv: arXiv:2201.07197. doi: 10.48550/arXiv.2201.07197.
	vi := v.Index
	lows := s.lows

	s.index += 2
	lows[vi] = s.index

	for _, w := range v.Successors {
		wi := w.Index

		if lows[wi] == 0 {
			s.scc(w)
		}

		if lows[wi] < lows[vi] {
			lows[vi] = lows[wi] | 1
		}
	}

	if lows[vi]&1 == 0 {
		leaderLow := lows[vi]

		scc := &Scc{
			Index: len(s.sccs),
		}
		F := s.F
		for lows[F[len(F)-1]] >= leaderLow {
			x := F[len(F)-1]
			F = F[:len(F)-1]

			lows[x] = math.MaxInt
			scc.Blocks = append(scc.Blocks, s.fn.Blocks[x])
			s.fn.Blocks[x].Scc = scc
		}
		s.F = F

		lows[vi] = math.MaxInt
		scc.Blocks = append(scc.Blocks, v)
		v.Scc = scc
		s.sccs = append(s.sccs, scc)
	} else {
		s.F = append(s.F, vi)
	}
}

// buildSccs computes the strongly connected components of fn's control flow graph.
func buildSccs(fn *Function) {
	n := len(fn.Blocks)

	if n == 0 {
		return
	}

	state := sccState{
		fn:    fn,
		index: 0,
		// The +1 makes space for the sentinel value stored in F.
		lows: make(BlockMap[int], n+1),
		F:    []int{n},
	}

	// All blocks are reachable from the entry block.
	state.scc(fn.Blocks[0])

	seen := make([]bool, len(state.sccs))
	for _, scc := range state.sccs {
		clear(seen)
		for _, b := range scc.Blocks {
			for _, succ := range b.Successors {
				if succ.Scc != b.Scc && !seen[succ.Scc.Index] {
					b.Scc.Successors = append(b.Scc.Successors, succ.Scc)
					succ.Scc.Predecessors = append(succ.Scc.Predecessors, b.Scc)
					seen[succ.Scc.Index] = true
				}
			}
		}
	}

	fn.Sccs = state.sccs

	// This iterates over the SCCs in reverse topological order, i.e., "bottom
	// up". This allows us to build up reachability sets.
	for _, scc := range fn.Sccs {
		scc.reachable.SetBit(&scc.reachable, scc.Index, 1)
		for _, succ := range scc.Successors {
			scc.reachable.Or(&scc.reachable, &succ.reachable)
		}
	}
}
