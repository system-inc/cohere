package control_flow_graph

// Solution is the settled boundary values of one dataflow, indexed by Block.Index.
type Solution[V any, E any] struct {

	// entry[i] is the value flowing into block i in the analysis's own direction: on entry to the
	// block for a Forward analysis, on exit from it for a Backward one.
	entry []V

	// exit[i] is the value flowing out of block i in the analysis's own direction, which is
	// Transfer applied to entry[i].
	exit []V

	reachable []bool
	rounds    int
}

// In returns the value flowing into block in the analysis's direction: on entry for a Forward
// analysis, on exit for a Backward one. The second result is false for an unreachable block.
func (solution *Solution[V, E]) In(block *Block[E]) (V, bool) {
	var zero V
	if solution == nil || block == nil || block.Index() >= len(solution.entry) || !solution.reachable[block.Index()] {
		return zero, false
	}
	return solution.entry[block.Index()], true
}

// Out returns the value flowing out of block in the analysis's direction. The second result is
// false for an unreachable block.
func (solution *Solution[V, E]) Out(block *Block[E]) (V, bool) {
	var zero V
	if solution == nil || block == nil || block.Index() >= len(solution.exit) || !solution.reachable[block.Index()] {
		return zero, false
	}
	return solution.exit[block.Index()], true
}

// Rounds reports how many times the solver swept every block before nothing moved.
//
// It is exposed because it is the measurement that says whether the iteration order is doing its
// job. A Forward analysis over an acyclic graph in reverse postorder settles in two rounds, one to
// propagate and one to observe nothing moved; more than that means back edges, and a lot more than
// that means the order is wrong or the lattice is taller than it looks.
func (solution *Solution[V, E]) Rounds() int {
	if solution == nil {
		return 0
	}
	return solution.rounds
}
