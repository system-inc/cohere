package high_level_intermediate_representation

import (
	"sort"

	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// mergeSweepState is the sweep's working state, matching upstream's `{joined, activeScopes}`.
type mergeSweepState struct {
	joined       scopeSet
	activeScopes []ScopeId
	starts       []scopesAtPosition
	ends         []scopesAtPosition
	rangeOf      func(ScopeId) mutation_aliasing.MutableRange

	// visits counts instruction positions handled, for the termination test only.
	visits int
}

// visitInstructionId advances the sweep to one evaluation position, transcribing upstream's
// `visitInstructionId`.
//
// Ends are processed before starts, which is upstream's order and is load-bearing: a scope ending at
// exactly the position another starts must leave the stack before the new one is pushed, or the two
// would appear to be simultaneously active and union spuriously.
func (state *mergeSweepState) visitInstructionId(id static_single_assignment.EvaluationOrder) {
	state.visits++

	if len(state.ends) > 0 && state.ends[len(state.ends)-1].position <= id {
		top := state.ends[len(state.ends)-1]
		state.ends = state.ends[:len(state.ends)-1]

		// Sorted by start descending: the scope that started latest is closed first, so that when
		// several scopes end together the unions below see the stack in the order upstream does.
		closing := append([]ScopeId(nil), top.scopes...)
		sortByStartDescending(closing, state.rangeOf)

		for _, scope := range closing {
			index := indexOfScope(state.activeScopes, scope)
			if index == -1 {
				continue
			}
			// A scope ending while something sits above it on the stack: everything above started
			// later and has not ended, so it interleaves with this one rather than nesting inside
			// it. Union them all.
			//
			// The `index != len-1` test is upstream's and it is a cost optimisation rather than a
			// filter. When the scope is already on top the suffix is empty, so the call reduces to
			// `union([scope])`, which inserts the scope as its own representative and nothing else;
			// `apply` then skips it because `group == scope`. Both a neutralised and a
			// forced-always-on mutation of this guard survive, and that is the correct verdict
			// rather than a fixture gap.
			//
			// Proven rather than argued, because "equivalent" is the verdict most likely to be
			// wrong: the whole corpus was run with the guard removed entirely and compared group by
			// group and range by range against this spelling. 685 functions, 88 unions, 0
			// differences. Upstream's spelling is kept because it is upstream's and because the
			// no-op insertion it avoids would otherwise grow `joined.order` with entries that mean
			// nothing.
			if index != len(state.activeScopes)-1 {
				state.joined.union(append([]ScopeId{scope}, state.activeScopes[index+1:]...))
			}
			state.activeScopes = append(state.activeScopes[:index], state.activeScopes[index+1:]...)
		}
	}

	if len(state.starts) > 0 && state.starts[len(state.starts)-1].position <= id {
		top := state.starts[len(state.starts)-1]
		state.starts = state.starts[:len(state.starts)-1]

		// Sorted by end descending, so the scope ending last goes deepest on the stack. That is the
		// ordering every union above depends on: "everything above me" is then exactly "everything
		// that ends before me".
		opening := append([]ScopeId(nil), top.scopes...)
		sortByEndDescending(opening, state.rangeOf)
		state.activeScopes = append(state.activeScopes, opening...)

		// Two scopes starting together and ending together cannot nest either way, so they are
		// unioned outright. Adjacent comparison suffices because the slice is sorted by end.
		//
		// A mutation inverting this comparison survives, and the reason is subsumption rather than a
		// fixture gap. Two scopes sharing a start and an end also share the end, so the end branch
		// above reaches them at that position with one of them not on top of the stack and unions
		// them there. Measured on the synthetic case that exercises it: with this union removed
		// entirely, `sameStartSameEnd` still collapses to one scope with one union.
		//
		// The corpus half of that verdict has now expired, exactly as predicted. It fired 0 times
		// when it was written, because no two scopes shared both endpoints. `AlignReactiveScopesToBlockScopes`
		// landed as the producer this paragraph named and runs in front of this pass, and over the
		// aligned table 251 adjacent pairs share both a start and an end. So the branch is now
		// reachable on the corpus and only the subsumption argument still explains the survival --
		// which is the stronger of the two reasons anyway, and it rests on the end branch's
		// ordering rather than on what the corpus happens to contain.
		//
		// Kept rather than deleted because it is upstream's and because the subsumption argument
		// rests on the end branch's ordering, which the same alignment pass could also change.
		for index := 1; index < len(opening); index++ {
			previous, current := opening[index-1], opening[index]
			if state.rangeOf(previous).End == state.rangeOf(current).End {
				state.joined.union([]ScopeId{previous, current})
			}
		}
	}
}

// visitPlace unions a place's scope with everything above it, transcribing upstream's `visitPlace`.
//
// This is the event with no counterpart in a pairwise-overlap model, and it is where most of the
// extra unions come from. A value read while its own scope is open but buried under a scope that
// opened later means the two genuinely interleave, even when their ranges nest perfectly.
func (state *mergeSweepState) visitPlace(
	id static_single_assignment.EvaluationOrder,
	place Place,
	scopes *ReactiveScopes,
	memberRanges *mutation_aliasing.MutableRanges,
) {
	placeScope := activeScopeOf(id, place, scopes)
	if placeScope == 0 {
		return
	}
	// Upstream's `isMutable(instr, place)`: the place's own recorded range must contain this
	// position. Reads the post-entanglement ranges, as upstream does at this point. See the doc
	// comment on `MergeOverlappingReactiveScopes`.
	if !memberRanges.Get(place.Identifier).Contains(id) {
		return
	}
	index := indexOfScope(state.activeScopes, placeScope)
	if index != -1 && index != len(state.activeScopes)-1 {
		state.joined.union(append([]ScopeId{placeScope}, state.activeScopes[index+1:]...))
	}
}

// apply widens each group representative and builds the result, transcribing upstream's apply loop.
//
// The widening is a plain min/max, which is upstream's spelling and is safe because `collectScopeInfo`
// refuses a scope whose start equals its end. See the doc comment above for the measurement.
func (state *mergeSweepState) apply(scopes *ReactiveScopes) *MergedScopes {
	result := &MergedScopes{
		ranges:  map[ScopeId]mutation_aliasing.MutableRange{},
		group:   map[ScopeId]ScopeId{},
		members: map[ScopeId][]static_single_assignment.IdentifierId{},
	}

	for _, scope := range scopes.Ids() {
		result.ranges[scope] = scopes.RangeOf(scope)
		result.group[scope] = scope
	}

	// Upstream's `joinedScopes.forEach`, walking insertion order so the representative is stable.
	for _, scope := range state.joined.order {
		group, ok := state.joined.find(scope)
		if !ok || group == scope {
			continue
		}
		result.group[scope] = group
		result.unions++

		widened := result.ranges[group]
		merged := scopes.RangeOf(scope)
		if merged.Start < widened.Start {
			widened.Start = merged.Start
		}
		if merged.End > widened.End {
			widened.End = merged.End
		}
		result.ranges[group] = widened
	}

	// A chain can put a scope's group behind another merge, so resolve to the final representative
	// before building membership. `find` has already compressed every path, so this is a lookup.
	for _, scope := range scopes.Ids() {
		if group, ok := state.joined.find(scope); ok {
			result.group[scope] = group
		}
	}

	for _, scope := range scopes.Ids() {
		group := result.group[scope]
		if group == scope {
			result.order = append(result.order, scope)
		} else {
			delete(result.ranges, scope)
		}
		result.members[group] = append(result.members[group], scopes.MembersOf(scope)...)
	}

	for _, group := range result.order {
		members := result.members[group]
		sort.Slice(members, func(left, right int) bool { return members[left] < members[right] })
		result.members[group] = members
	}

	return result
}
