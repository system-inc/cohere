package react

import (
	"github.com/system-inc/cohere/internal/lint/ecmascript/high_level_intermediate_representation"
	"github.com/system-inc/cohere/static_single_assignment"
)

// immutabilityState is the abstract interpretation's state.
//
// Keyed by value rather than by binding, with `aliases` mapping a binding onto the value it names.
// That indirection is upstream's `variables`/`values` split and it is what makes freezing propagate
// through an alias: `const alias = o; useFoo(alias); o.a = 1;` reports because `alias` and `o` name
// one value, and freezing reaches the value rather than the name.
type immutabilityState struct {
	// values holds each value's abstract kind and reasons, keyed by the identifier that introduced
	// it. A binding reaches its value through `aliases`.
	values map[static_single_assignment.IdentifierId]immutabilityAbstractValue
	// aliases maps a binding onto the value it ultimately names.
	aliases map[static_single_assignment.IdentifierId]static_single_assignment.IdentifierId
	// declarationValues recovers a value whose single-assignment numbering did not unify, keyed by
	// the source binding. refs.go documents the same representation gap and works around it the
	// same way.
	declarations  map[static_single_assignment.IdentifierId]static_single_assignment.DeclarationId
	byDeclaration map[static_single_assignment.DeclarationId]static_single_assignment.IdentifierId
	// names carries a source binding name onto the temporary that loaded it.
	names map[static_single_assignment.IdentifierId]string
	// functionsReassigning records, per value, which locals calling it would reassign. A function
	// value carrying a non-empty set is a "known mutable function".
	functionsReassigning map[static_single_assignment.IdentifierId][]immutabilityReassignment
	// functionCaptures records what a closure closes over, so freezing the closure freezes them.
	functionCaptures map[static_single_assignment.IdentifierId][]static_single_assignment.IdentifierId
	// aggregateElements records what an object or array literal holds, so freezing the aggregate
	// freezes its contents. Kept separate from functionCaptures because an object holding a value
	// and a closure capturing one are different relations that happen to propagate the same way.
	aggregateElements map[static_single_assignment.IdentifierId][]static_single_assignment.IdentifierId
	// changed is the fixpoint's only termination signal.
	changed bool
	// insideLoop is true while the sweep is walking a block that a back edge can re-enter.
	insideLoop bool
	// frozenInsideLoop records values frozen at such a point, which are the only ones a back edge
	// may carry into the next round. See the sweep.
	frozenInsideLoop map[static_single_assignment.IdentifierId]bool
}

// resolve follows a binding to the value it names.
//
// Bounded rather than a bare loop: an alias chain is acyclic by construction in single-assignment
// form, but this pass also seeds aliases from a declaration fallback, and a defensive bound costs
// nothing next to a hang in a linter.
func (state *immutabilityState) resolve(
	id static_single_assignment.IdentifierId,
) static_single_assignment.IdentifierId {
	current := id
	for round := 0; round < 32; round++ {
		next, found := state.aliases[current]
		if !found || next == current {
			return current
		}
		current = next
	}
	return current
}

// alias records that one binding names the same value as another.
func (state *immutabilityState) alias(
	from static_single_assignment.IdentifierId,
	to static_single_assignment.IdentifierId,
) {
	resolved := state.resolve(to)
	if resolved == from {
		return
	}
	if existing, found := state.aliases[from]; found && existing == resolved {
		return
	}
	state.aliases[from] = resolved
	state.changed = true
}

// kindOf is a value's current abstract value, defaulting to Mutable.
//
// Mutable is the default rather than a bottom element because an unseen value is one this code has
// not been shown to have frozen, and the rule reports only on freezing. Defaulting to anything
// else would report values nothing had frozen.
func (state *immutabilityState) kindOf(id static_single_assignment.IdentifierId) immutabilityAbstractValue {
	resolved := state.resolve(id)
	if found, ok := state.values[resolved]; ok {
		return found
	}
	// Fall back to whatever the same source binding holds elsewhere in this function, which
	// recovers a value whose single-assignment numbering did not unify with the one that was
	// written. refs.go measures the same gap on its own corpus.
	if declaration, ok := state.declarations[resolved]; ok {
		if other, ok := state.byDeclaration[declaration]; ok && other != resolved {
			if found, ok := state.values[other]; ok {
				return found
			}
		}
	}
	return immutabilityAbstractValue{Kind: immutabilityMutable}
}

// setKind writes a value's abstract value, recording whether the kind moved.
//
// This is the convergence test and it deliberately ignores the reason set. See the rule comment:
// reasons only ever grow, so counting a reason arrival as movement makes any function with a loop
// burn the whole bound.
func (state *immutabilityState) setKind(id static_single_assignment.IdentifierId, value immutabilityAbstractValue) {
	resolved := state.resolve(id)
	previous, existed := state.values[resolved]
	merged := value
	if existed {
		merged = immutabilityMergeValues(previous, value)
	}
	if !existed || previous.Kind != merged.Kind {
		state.changed = true
	}
	state.values[resolved] = merged
	if declaration, ok := state.declarations[resolved]; ok {
		state.byDeclaration[declaration] = resolved
	}
}

// freeze marks a value frozen for a reason, which is the whole engine of this rule.
//
// A value that is already Frozen, Global or Primitive is left alone, matching upstream's `freeze`:
// re-freezing an already-frozen value must not count as movement or the fixpoint never settles.
func (state *immutabilityState) freeze(id static_single_assignment.IdentifierId, reason immutabilityReason) {
	current := state.kindOf(id)
	switch current.Kind {
	case immutabilityFrozen, immutabilityGlobal, immutabilityPrimitive:
		// Already settled. Absorb the reason without moving the kind.
		if current.Reasons&reason == 0 {
			resolved := state.resolve(id)
			current.Reasons |= reason
			state.values[resolved] = current
		}
		return
	}
	state.setKind(id, immutabilityAbstractValue{Kind: immutabilityFrozen, Reasons: current.Reasons | reason})
	// Only a freeze that happened inside a loop body may travel around the back edge into the next
	// round. See the sweep for why carrying every freeze forward reports ordinary accumulator loops.
	if state.insideLoop {
		state.frozenInsideLoop[state.resolve(id)] = true
	}
	// Freezing a closure freezes what it captured, which is upstream's transitive freeze. Guarded
	// against re-entry through the captures map itself so a closure capturing another closure that
	// captures it cannot recurse forever.
	state.freezeInward(id, reason)
}

// freezeInward distributes a freeze to what a value holds: a closure's captures, an aggregate's
// elements. Both maps are removed for the duration of the descent rather than guarded by a visited
// set, so a structure that contains itself cannot recurse forever, and are restored afterwards so a
// second freeze for a different reason still reaches inside.
func (state *immutabilityState) freezeInward(id static_single_assignment.IdentifierId, reason immutabilityReason) {
	resolved := state.resolve(id)
	captures, hasCaptures := state.functionCaptures[resolved]
	elements, hasElements := state.aggregateElements[resolved]
	if !hasCaptures && !hasElements {
		return
	}
	delete(state.functionCaptures, resolved)
	delete(state.aggregateElements, resolved)
	for _, inner := range captures {
		state.freeze(inner, reason)
	}
	for _, inner := range elements {
		state.freeze(inner, reason)
	}
	if hasCaptures {
		state.functionCaptures[resolved] = captures
	}
	if hasElements {
		state.aggregateElements[resolved] = elements
	}
}

// noteDeclaration records which source binding a value belongs to.
func (state *immutabilityState) noteDeclaration(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) {
	if function == nil || int(id) >= len(function.Identifiers) {
		return
	}
	if identifier := function.Identifiers[id]; identifier != nil && identifier.Name != "" {
		state.declarations[id] = identifier.Declaration
		if _, seen := state.byDeclaration[identifier.Declaration]; !seen {
			state.byDeclaration[identifier.Declaration] = id
		}
	}
}

// nameOf is the value's own binding name, or the one a load carried onto it.
func (state *immutabilityState) nameOf(
	function *high_level_intermediate_representation.Function,
	id static_single_assignment.IdentifierId,
) string {
	// `function` is nil at one call site, where only the side maps are wanted. `immutability
	// IdentifierName` already declines a nil function rather than panicking, so this is safe and is
	// stated here because a nil receiver in a name lookup reads like an oversight.
	if name := immutabilityIdentifierName(function, id); name != "" {
		return name
	}
	if name, ok := state.names[id]; ok {
		return name
	}
	return state.names[state.resolve(id)]
}
