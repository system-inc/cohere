package mutation_aliasing

import "github.com/system-inc/cohere/static_single_assignment"

// AliasingGraph is the alias graph of one function, built and not yet applied: the state the walk
// ended in, and the mutations it collected. A value, so building one allocates no wrapper.
type AliasingGraph struct {
	state     *aliasingState
	mutations []pendingMutation
}

// Kind is the abstract kind the walk ended with for a value: an immutable kind, or Mutable for a
// value it holds none for, which is also what a value the walk never reached reads as.
func (graph AliasingGraph) Kind(id static_single_assignment.IdentifierId) EffectValueKind {
	return graph.state.immutable[id]
}

// MutationCount is how many mutations the walk kept, after the ones upstream drops (a conditional
// mutation of a value that is not Mutable or Context).
func (graph AliasingGraph) MutationCount() int {
	return len(graph.mutations)
}

// Widen is upstream's Part 1 applied: every mutation the graph collected, walked through it, widening
// the ranges in result.
//
// Separated from the definition half so the two run in upstream's order rather than interleaved. See
// `InferMutableRanges` for why the order is load-bearing.
//
// A mutation is applied only when its instruction carries an evaluation order. An unnumbered
// instruction means `Finalize` never reached the block, and widening to `0 + 1` there would write a
// range end of 1 onto a value whose start is unset, producing a value that reads as mutable at
// position 0 in a function where nothing has an order at all.
func (graph AliasingGraph) Widen(result *MutableRanges) {
	for _, mutation := range graph.mutations {
		if mutation.end == 0 {
			// `end` is `instruction.Order + 1`, so zero is impossible for a numbered instruction and
			// this only fires on an unnumbered one, where `Order + 1` is 1. Guarded on the order
			// itself below rather than here.
			continue
		}
		if mutation.end == 1 {
			// Order was zero: unnumbered. See the doc comment.
			continue
		}
		graph.state.mutate(
			result,
			mutation.index,
			mutation.place,
			mutation.end,
			true,
			mutation.transitive,
			mutation.kind,
		)
	}
}
