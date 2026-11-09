package forward_flow

import (
	"honnef.co/go/tools/analysis/dfa"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// forwardBuilder is the state of one Forward analysis.
type forwardBuilder[Lattice dfa.Semilattice[Fact], Fact any] struct {
	lattice Lattice

	function *intermediate_representation.Function

	// transfer is the edge transfer function.
	transfer func(from int, to int, fact Fact) Fact

	blocks []blockInformation[Fact]

	queue nodeHeap
}

// blockInformation is what the analysis holds of one block.
type blockInformation[Fact any] struct {
	// dirty is whether the block's in fact has never been propagated.
	dirty bool

	predecessors []blockEdge

	in Fact
	// out is the fact on each edge out of the block, in the order of its successors.
	out []Fact
}

// blockEdge is an edge into a block: the block it leaves and its place among that block's edges out.
type blockEdge struct {
	node     int
	outIndex int
}

// merge is the merge of left and right, left itself when the two are equal.
func (builder *forwardBuilder[Lattice, Fact]) merge(left Fact, right Fact) Fact {
	if builder.lattice.Equals(left, right) {
		return left
	}
	return builder.lattice.Merge(left, right)
}

// propagate visits the queued blocks until no fact changes: each block's in fact is the merge of the facts on its
// edges in, and when that has changed, or the block was never visited, each edge out carries what transfer makes of
// it, its successor enqueued when that changed.
func (builder *forwardBuilder[Lattice, Fact]) propagate() {
	for builder.queue.pending() {
		node := builder.queue.dequeue()
		block := &builder.blocks[node]

		// Merge the predecessors' facts into the updated in fact.
		var in Fact
		first := true
		for _, edge := range block.predecessors {
			predecessor := &builder.blocks[edge.node]
			var edgeFact Fact
			if predecessor.dirty {
				// A predecessor not yet visited has no meaningful out facts.
				edgeFact = builder.lattice.Ident()
			} else {
				edgeFact = predecessor.out[edge.outIndex]
			}
			if first {
				in = edgeFact
				first = false
			} else {
				in = builder.merge(in, edgeFact)
			}
		}
		if first {
			// A block with no predecessors keeps its initial state.
			in = block.in
		}

		if !block.dirty && builder.lattice.Equals(in, block.in) {
			// The block's in fact has not changed, so neither would what transfer makes of it.
			continue
		}
		block.in = in

		outIndex := 0
		for successor := range builder.function.Out(node) {
			edgeFact := builder.transfer(node, successor, in)
			if block.dirty || !builder.lattice.Equals(block.out[outIndex], edgeFact) {
				// The out fact changed, so the successor is visited again.
				block.out[outIndex] = edgeFact
				builder.queue.enqueue(successor)
			}
			outIndex++
		}

		block.dirty = false
	}
}
