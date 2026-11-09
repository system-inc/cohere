// Redundant phi elimination.
//
// A phi is redundant when it is not actually a merge: every operand is the same value, or every
// operand is either the same value or the phi's own result. The second case is what a loop produces
// when the body does not reassign the variable - the back edge feeds the phi its own output - and it
// is the reason this cannot be a simple "are all operands equal" test.
//
// # Why this is here rather than left to the next agent
//
// Braun's construction places a phi whenever a lookup crosses a join, before it knows whether the
// operands will agree. On a loop it must: the header's phi is minted before the back edge's value
// exists. So the construction cannot avoid producing redundant phis, and a redundant phi is not
// cosmetic - it is a value that appears to have several reaching definitions when it has one, which
// is precisely the fact every pass above this will key on. Shipping construction without it would
// mean every consumer re-deriving "is this phi real".
//
// Removal cascades: deleting one phi can make another redundant, since a phi's operand may be the
// removed phi's result. So this iterates to a fixed point rather than making one pass.
//
// Upstream's equivalent is `eliminate_redundant_phi.rs`, 177 lines.
//
// # A cycle of phis the one-at-a-time rule can't see
//
// Irreducible control flow, a loop entered at two blocks as Go's `goto` can make, gives each entry
// block a phi whose operands are the value from before the loop and the other entry's phi. Each phi
// sees two distinct operands, so neither is trivial, yet together they only ever hold the one value
// from outside. Braun et al. (CC 2013, section 3.2) name this a redundant set: phis that reference
// only each other and one other value. Their Lemma 1 shows every redundant set holds a strongly
// connected component that is redundant on its own, so their Algorithm 5 looks there:
//
//  1. Take the graph of the phis, with an edge from each phi to each operand that is a phi.
//  2. Visit its strongly connected components with the components a phi uses before the phi's own,
//     so an operand from outside the current component has already been collapsed if it could be.
//  3. A component of one phi was the trivial rule's to judge, and is left. For a larger one, gather
//     the operands from outside it. One value means every phi in the component is that value. More
//     than one means each phi with an outside operand is a real merge, and the phis whose operands
//     all lie inside (the inner phis) may still hold a redundant component of their own, so the step
//     recurses on them. None means nothing reaches the component, and it is left.
//
// The trivial rule then runs again, since a collapsed component can leave a phi trivial, and the two
// alternate until neither removes anything. JavaScript and TypeScript have no `goto`, so their flow is
// reducible and this step should find nothing there; it is here for Go and for Adamic.
package static_single_assignment

// EliminateRedundantPhis removes phis that are not merges, rewriting every reference to a removed
// phi's result to the value it collapsed to.
//
// Runs to a fixed point over one function; an IR recurses into nested functions itself. The trivial
// rule runs to its own fixed point first, then the strongly connected step, and the two alternate
// until neither removes a phi. The phis kept stay in their blocks in the order they were in.
func EliminateRedundantPhis[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) {
	for {
		for eliminateTrivialPhis(graph, function) {
		}
		if !eliminateRedundantComponents(graph, function) {
			return
		}
	}
}

// eliminateTrivialPhis removes every phi the trivial rule finds redundant in one pass over the
// function, rewrites the references to them, and reports whether it removed any.
func eliminateTrivialPhis[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) bool {
	// rewrites maps a removed phi's result to what it collapsed to.
	rewrites := map[IdentifierId]IdentifierId{}

	for _, block := range graph.Blocks(function) {
		phis := graph.Phis(block)
		kept := phis[:0]
		for _, phi := range phis {
			if collapsed, redundant := redundantPhiValue(graph, phi); redundant {
				rewrites[graph.IdentifierOf(phi.Place)] = collapsed
				continue
			}
			kept = append(kept, phi)
		}
		graph.SetPhis(block, kept)
	}

	if len(rewrites) == 0 {
		return false
	}
	applyRewrites(graph, function, rewriteChain(rewrites))
	return true
}

// rewriteChain resolves an identifier through rewrites to the end of its chain.
//
// A removed phi may collapse to another removed phi's result, so each chain is chased to its end
// before rewriting. The chain is acyclic because a phi only collapses to a value that is not itself,
// and a component only to a value outside it.
func rewriteChain(rewrites map[IdentifierId]IdentifierId) func(IdentifierId) IdentifierId {
	return func(id IdentifierId) IdentifierId {
		current := id
		seen := 0
		for {
			next, ok := rewrites[current]
			if !ok {
				return current
			}
			current = next
			seen++
			if seen > len(rewrites) {
				// Defensive: a cycle cannot arise from the redundancy tests, but a caller that
				// hand-built phis could create one, and looping forever is the worst possible response.
				return current
			}
		}
	}
}

// eliminateRedundantComponents is Braun et al.'s Algorithm 5 over the function's phis, once: it removes
// every strongly connected component of phis that holds only one value from outside it, rewrites the
// references to them, and reports whether it removed any. The file comment says why.
func eliminateRedundantComponents[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) bool {
	// A component this step can collapse has two phis at least, each reaching the other. Most functions
	// have fewer, and this runs for every one, so they are counted before anything is gathered.
	count := 0
	for _, block := range graph.Blocks(function) {
		count += len(graph.Phis(block))
	}
	if count < 2 {
		return false
	}
	components := phiComponents[G, F, B, P]{graph: graph, phis: make([]*Phi[P], 0, count)}
	for _, block := range graph.Blocks(function) {
		components.phis = append(components.phis, graph.Phis(block)...)
	}
	components.index = make(map[IdentifierId]int, len(components.phis))
	for index, phi := range components.phis {
		components.index[graph.IdentifierOf(phi.Place)] = index
	}
	if !components.anyPhiOperand() {
		return false
	}

	components.rewrites = map[IdentifierId]IdentifierId{}
	components.resolve = rewriteChain(components.rewrites)
	components.member = make([]int, count)
	components.order = make([]int, count)
	components.lowest = make([]int, count)
	components.onStack = make([]bool, count)
	components.component = make([]int, count)
	every := make([]int, count)
	for index := range every {
		every[index] = index
	}
	components.removeRedundant(every)
	if len(components.rewrites) == 0 {
		return false
	}

	for _, block := range graph.Blocks(function) {
		phis := graph.Phis(block)
		kept := phis[:0]
		for _, phi := range phis {
			if _, removed := components.rewrites[graph.IdentifierOf(phi.Place)]; !removed {
				kept = append(kept, phi)
			}
		}
		graph.SetPhis(block, kept)
	}
	applyRewrites(graph, function, components.resolve)
	return true
}

// redundantPhiValue reports the single value a phi collapses to, if it collapses at all.
//
// The rule, which is upstream's: ignoring operands that are the phi's own result, if every
// remaining operand names one value, the phi is that value. A phi with no operands other than
// itself cannot arise from a reachable block and is treated as not redundant so it stays visible.
func redundantPhiValue[G Graph[F, B, P], F any, B comparable, P any](graph G, phi *Phi[P]) (IdentifierId, bool) {
	result := graph.IdentifierOf(phi.Place)
	var candidate IdentifierId
	found := false
	for _, entry := range phi.Operands {
		operand := graph.IdentifierOf(entry.Place)
		if operand == result {
			continue
		}
		if !found {
			candidate = operand
			found = true
			continue
		}
		if operand != candidate {
			return 0, false
		}
	}
	if !found {
		return 0, false
	}
	return candidate, true
}

// applyRewrites rewrites every place in the function through resolve.
func applyRewrites[G Graph[F, B, P], F any, B comparable, P any](
	graph G,
	function F,
	resolve func(IdentifierId) IdentifierId,
) {
	rewrite := func(place *P, role Role) {
		*place = graph.WithIdentifier(*place, resolve(graph.IdentifierOf(*place)))
	}
	for _, block := range graph.Blocks(function) {
		for _, phi := range graph.Phis(block) {
			rewrite(&phi.Place, Define)
			for index := range phi.Operands {
				rewrite(&phi.Operands[index].Place, Use)
			}
		}
		count := graph.InstructionCount(function, block)
		for index := 0; index < count; index++ {
			graph.EachInstructionPlace(function, block, index, rewrite)
		}
		graph.EachTerminalPlace(block, rewrite)
	}
	parameters := graph.Parameters(function)
	for index := range parameters {
		rewrite(&parameters[index], Define)
	}
	if returns := graph.Returns(function); returns != nil {
		rewrite(returns, Use)
	}
}
