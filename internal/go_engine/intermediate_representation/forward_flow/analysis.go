// Package forward_flow solves a forward monotone data flow problem over a function's control flow graph in the
// engine's own intermediate representation, for the fact passes ported from staticcheck that ran one over buildir's
// (#w7zmvat).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/dfa/dense (forward.go and flow.go) and the
// parts of internal/xtools-internal/graph those call, compaction and the reverse postorder, keeping only what
// return_nil_ness reads: Forward and the fact on entry to each block. It keeps the original's order step for step,
// since a transfer function need not be monotone and the order blocks are visited in can then change the answer:
// blocks are swept in reverse postorder, a block reached again by an edge back to one already passed in a sweep waits
// for the next sweep, and a block whose incoming fact has not changed since its last visit is not visited again. A
// function's blocks are numbered from zero in order, which the original's compaction maps to themselves, so each node
// here is its block's index. The lattice is the original's own dfa.Semilattice, imported as is, since it reads no
// representation.
package forward_flow

import (
	"slices"

	"honnef.co/go/tools/analysis/dfa"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// Analysis is the outcome of a forward analysis: the fact on entry to each block.
type Analysis[Fact any] struct {
	// ins is the fact on entry to each block, by the block's index.
	ins []Fact
}

// In is the fact on entry to the block at index block, the merge of the facts on every edge into it.
func (analysis *Analysis[Fact]) In(block int) Fact {
	return analysis.ins[block]
}

// Forward runs a forward monotone analysis over function's control flow graph. The entry map gives the fact on entry
// to a block with no predecessors, Lattice's identity where it gives none. For each edge it calls transfer(from, to,
// fact), with the blocks' indices and the fact on entry to from, which returns the fact the edge carries to to.
func Forward[Lattice dfa.Semilattice[Fact], Fact any](
	function *intermediate_representation.Function,
	entry map[int]Fact,
	transfer func(from int, to int, fact Fact) Fact,
) *Analysis[Fact] {
	nodeCount := function.NumNodes()
	builder := &forwardBuilder[Lattice, Fact]{
		function: function,
		transfer: transfer,
		blocks:   make([]blockInformation[Fact], nodeCount),
	}
	builder.queue.initialize(function)

	// Initialize each node.
	for node := range nodeCount {
		block := &builder.blocks[node]

		// The edges into each block, which the graph does not list, are gathered from the edges out of every block.
		outs := 0
		for successor := range function.Out(node) {
			successorBlock := &builder.blocks[successor]
			successorBlock.predecessors = append(successorBlock.predecessors, blockEdge{node: node, outIndex: outs})
			outs++
		}

		fact, hasEntry := entry[node]
		if !hasEntry {
			fact = builder.lattice.Ident()
		}
		block.in = fact
		block.out = slices.Repeat([]Fact{builder.lattice.Ident()}, outs)

		// Every block is enqueued, not only the entry ones: the entry map may hold no interesting state, and the
		// transfer function may still introduce one anywhere.
		block.dirty = true
		builder.queue.enqueue(node)
	}

	builder.propagate()

	analysis := &Analysis[Fact]{ins: make([]Fact, nodeCount)}
	for node := range nodeCount {
		analysis.ins[node] = builder.blocks[node].in
	}
	return analysis
}
