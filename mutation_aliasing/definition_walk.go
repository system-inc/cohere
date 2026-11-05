package mutation_aliasing

import "github.com/system-inc/cohere/static_single_assignment"

// definitionWalk is the definition half's one place visitor and what it reads: which of the two
// loops over an instruction's places it is in, and the instruction's order.
type definitionWalk[G Graph[F, B, P], F any, B comparable, P any] struct {
	graph   G
	result  *MutableRanges
	order   static_single_assignment.EvaluationOrder
	lvalues bool
}

// visitPlace opens an lvalue's range in the first loop and a widened operand's start in the second.
func (walk *definitionWalk[G, F, B, P]) visitPlace(place *P, role static_single_assignment.Role) {
	if walk.lvalues {
		if role == static_single_assignment.Define {
			walk.openLValue(walk.graph.IdentifierOf(*place))
		}
		return
	}
	if role == static_single_assignment.Define {
		return
	}
	id := walk.graph.IdentifierOf(*place)
	existing := walk.result.Get(id)
	if existing.End > walk.order && existing.Start == 0 {
		existing.Start = walk.order
		walk.result.Set(id, existing)
	}
}

// openLValue opens the range of a value an instruction defines, the lvalue loop's body.
func (walk *definitionWalk[G, F, B, P]) openLValue(id static_single_assignment.IdentifierId) {
	order := walk.order
	existing := walk.result.Get(id)
	changed := false
	if existing.Start == 0 {
		existing.Start = order
		changed = true
	}
	if existing.End == 0 {
		existing.End = maximumOrder(order+1, existing.End)
		changed = true
	}
	// Divergence from both upstreams, in the conservative direction, and it is the only
	// one this stage introduces. See `TestRangesStayValidAcrossALoopBackEdge`.
	//
	// Upstream's two guards above are each conditioned on their own field being unset,
	// so a value whose `End` was widened by a mutation and whose `Start` is opened here
	// keeps both writes independently. On a loop-carried value that produces an
	// interval whose end is at or before its start: the back-edge phi operand is
	// defined at a high evaluation order, while the mutation that reaches it through
	// the phi happened at a lower one. Measured minimal case, `for (let i = 0; ...;
	// i++)` with a mutating call in the body: `i`'s last value is defined at order 23
	// and the `i++` mutation widens its end to 23, giving the empty interval [23,23).
	//
	// React asserts against exactly this in `validateMutableRange` (line 20999), and
	// that assertion is not reachable by default -- `assertValidMutableRanges` is a
	// config flag defaulting to false (line 31643), so upstream ships the state it
	// declares invalid. Measured on this corpus before the clamp: 48 values across 400
	// files, every one of them loop-carried.
	//
	// Reproducing that faithfully would mean shipping intervals on which `Contains`
	// answers false at every position: a consumer asking whether the value is still
	// being written gets a confident no at exactly the positions where it demonstrably
	// was. So the range is unset instead, and `RangeGapLoopCarriedInversion` names it.
	//
	// The alternative considered and rejected was clamping the end to `start + 1`.
	// That keeps the interval non-empty and satisfies the invariant, and it is wrong
	// for two reasons. It writes down a fact we do not have: the true range of a
	// loop-carried value starts before the back-edge store, and how far before is
	// precisely what the folded phi hides, so `[23,24)` is not a narrower version of
	// the real range, it is a different range that happens to be non-empty. And a
	// clamp satisfies `IsValid` by construction, which launders every producer bug
	// into a valid-looking range and leaves `ValidateMutableRanges` unable to fail.
	//
	// Unset costs nothing at the call site, which is what makes it the honest choice
	// rather than a cautious one: `Contains` already answers false for an unset range,
	// so every consumer behaves exactly as it would have under the clamp. The only
	// thing that changes is what this pass claims to know.
	if existing.IsSet() && existing.End <= existing.Start {
		existing = MutableRange{}
		changed = true
	}
	if changed {
		walk.result.Set(id, existing)
	}
}
