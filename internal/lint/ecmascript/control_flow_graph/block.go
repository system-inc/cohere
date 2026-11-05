package control_flow_graph

// Block is one basic block: the events its consumer recorded, in evaluation
// order, and the blocks led to from it.
//
// Successors spans the whole graph, unreachable blocks included, so a walk that
// only wants the code that runs skips the blocks whose Reachable is false.
type Block[E any] struct {
	Events     []E
	Successors []*Block[E]

	// Reachable reports whether control can arrive here. The code after an
	// abrupt exit is laid out in unreachable blocks rather than dropped, and a
	// consumer's hooks run in them, so a rule can ask what would have run had
	// the exit not been there.
	//
	// It is settled when the block is entered, from the edges that lead into
	// it by then. Every block is entered after the edges that could make it
	// reachable exist: a block only ever gains an edge afterwards from code
	// laid out inside it, which is unreachable whenever the block itself is.
	Reachable bool

	index        int32
	hasIncoming  bool
	final        bool
	thrown       bool
	cycleBarrier []bool
	successors   [2]*Block[E]
}

// Index reports the block's position in Graph.Blocks, so a consumer can hold
// per-block analysis state in a slice rather than a map.
func (blk *Block[E]) Index() int {
	return int(blk.index)
}
