package control_flow_graph

// DominatorAnalysis holds the dominator tree of one graph's reachable subgraph, plus the
// predecessor edges it was computed from.
//
// It is separate from PathAnalysis rather than folded into it because the two answer different
// questions over different graphs. PathAnalysis dominates over a graph with a synthetic exit and no
// thrown edges, to answer "is this on every non-throwing final path". This dominates over the real
// reachable successor graph, to answer "does control have to pass through A to reach B". A consumer
// wanting the second and given the first would get thrown paths silently excluded.
type DominatorAnalysis[E any] struct {
	graph *Graph[E]

	// immediate[i] is the block index of block i's immediate dominator, or -1 when block i is
	// unreachable. The entry block is its own immediate dominator, which is the convention
	// Cooper-Harvey-Kennedy uses as its fixed point and which `Dominates` accounts for.
	immediate []int

	// rpoPosition[i] is block i's index in reverse postorder, or -1 when unreachable. It is kept
	// because `Dominates` walks the tree upward and needs no ordering, but `ReversePostOrder`
	// hands it out and the intersect step needed it anyway.
	rpoPosition []int

	// reversePostOrder is the reachable blocks in reverse postorder: every block appears after at
	// least one predecessor, except the entry and the targets of back edges. It is the iteration
	// order a forward dataflow wants, and the order the liveness solver in `no-useless-assignment`
	// documents itself as approximating with a descending-index walk.
	reversePostOrder []*Block[E]

	// predecessors is one flat array of every reachable in-edge, grouped by target block, with
	// predecessorStart[i]..predecessorStart[i+1] delimiting block i's group. One allocation rather
	// than one slice per block, which is the same counting-sort shape `findFinalPathDominators`
	// already uses internally.
	predecessors     []*Block[E]
	predecessorStart []int
}

// Predecessors returns the reachable blocks with an edge into block, in Blocks order.
//
// The graph stores only successors, because that is the direction the builder lays edges down in.
// Every backward analysis needs the other direction, and every consumer that has needed it so far
// has rebuilt it: `findFinalPathDominators` builds a counting-sorted predecessor array and discards
// it, and the liveness solver in `no-useless-assignment` avoids needing one only by iterating every
// block every round.
//
// The result is computed once by `AnalyzeDominators` and shared, so this is a slice read rather
// than a walk. It is owned by the analysis and must not be mutated: two callers see the same slice.
//
// Unreachable blocks have no predecessors here even when a successor edge points at them. That
// mirrors what every existing consumer already does (each one skips `!successor.Reachable` at its
// own loop), and it is the definition dominance needs, since a block nothing arrives at cannot
// dominate anything.
func (analysis *DominatorAnalysis[E]) Predecessors(block *Block[E]) []*Block[E] {
	if analysis == nil || block == nil || block.Index() >= len(analysis.predecessorStart)-1 {
		return nil
	}
	start := analysis.predecessorStart[block.Index()]
	end := analysis.predecessorStart[block.Index()+1]
	return analysis.predecessors[start:end]
}

// IsReachable reports whether block participates in this analysis at all.
//
// Every other accessor answers false or nil for an unreachable block, and that answer is
// indistinguishable from a real negative. This is how a caller tells the two apart.
func (analysis *DominatorAnalysis[E]) IsReachable(block *Block[E]) bool {
	if analysis == nil || block == nil || block.Index() >= len(analysis.immediate) {
		return false
	}
	return analysis.immediate[block.Index()] != -1
}

// ImmediateDominator returns the block that immediately dominates block: the last block every path
// from the entry to block passes through, other than block itself.
//
// The second result is false for an unreachable block and for the entry block, which has no
// immediate dominator. Returning the entry as its own would make a caller walking the tree upward
// loop forever, and callers do walk it upward.
func (analysis *DominatorAnalysis[E]) ImmediateDominator(block *Block[E]) (*Block[E], bool) {
	if !analysis.IsReachable(block) {
		return nil, false
	}
	index := block.Index()
	if index == 0 {
		return nil, false
	}
	return analysis.graph.Blocks[analysis.immediate[index]], true
}

// Dominates reports whether every path from the graph entry to block passes through dominator.
//
// A block dominates itself, which is the standard convention and the one Cooper-Harvey-Kennedy's
// fixed point produces. `StrictlyDominates` is the version that excludes it.
//
// Both blocks must be reachable. An unreachable block is dominated by nothing and dominates
// nothing, so either being unreachable answers false.
func (analysis *DominatorAnalysis[E]) Dominates(dominator, block *Block[E]) bool {
	if !analysis.IsReachable(dominator) || !analysis.IsReachable(block) {
		return false
	}
	target := dominator.Index()
	// Walking the tree upward rather than comparing depth intervals. The chain is bounded by the
	// tree's height and this is not on a hot path; a depth-interval encoding would need a second
	// pass to build and would have to be kept honest against the tree.
	for index := block.Index(); ; index = analysis.immediate[index] {
		if index == target {
			return true
		}
		if index == 0 {
			return false
		}
	}
}

// StrictlyDominates reports whether dominator dominates block and is not block.
func (analysis *DominatorAnalysis[E]) StrictlyDominates(dominator, block *Block[E]) bool {
	return dominator != block && analysis.Dominates(dominator, block)
}

// ReversePostOrder returns the reachable blocks in reverse postorder, entry first.
//
// This is the iteration order a forward dataflow wants: every block appears after at least one of
// its predecessors, except the entry and the targets of back edges, so one pass propagates as far
// as the acyclic structure allows and the fixed point converges in the number of rounds the loop
// nesting depth sets rather than the block count.
//
// A backward dataflow wants this reversed, which `Solve` does rather than making each caller
// remember to.
func (analysis *DominatorAnalysis[E]) ReversePostOrder() []*Block[E] {
	if analysis == nil {
		return nil
	}
	return analysis.reversePostOrder
}
