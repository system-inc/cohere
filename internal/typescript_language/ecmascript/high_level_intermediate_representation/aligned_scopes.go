package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// AlignedScopes is what this pass produces: the widened scope ranges, plus the member ranges
// re-derived from them.
//
// # Why this carries member ranges, which is `ScopeGapPostAlignmentWidening` coming due
//
// `AssignReactiveScopes` records that React aliases every member's `mutableRange` to the scope's own
// range object, so a later widening of the scope retroactively widens every member. `MutableRange` is
// a value type here and that pass stores a copy, which was indistinguishable from the alias for
// exactly as long as nothing widened a scope range afterwards. This pass is the first thing that
// does, so the alias and the copy are now distinguishable and the gap is live.
//
// Member ranges are therefore RE-derived from the widened ranges rather than inherited from
// `ReactiveScopes.MemberRanges()`, which still reports the pre-alignment hull. A consumer reading
// member ranges after this pass must take them from here. `TestAlignReDerivesMemberRanges` measures
// the two tables against each other and asserts they differ, because a re-derivation that changed
// nothing would mean this pass had not widened anything.
//
// The zero value is an empty result and is ready to read.
type AlignedScopes struct {
	ranges  map[ScopeId]mutation_aliasing.MutableRange
	members map[ScopeId][]static_single_assignment.IdentifierId
	order   []ScopeId
	widened int
	visits  int
}

// RangeOf returns a scope's aligned range, or the unset range for a scope that does not exist.
func (scopes *AlignedScopes) RangeOf(scope ScopeId) mutation_aliasing.MutableRange {
	if scopes == nil || scopes.ranges == nil {
		return mutation_aliasing.MutableRange{}
	}
	return scopes.ranges[scope]
}

// MembersOf returns the values belonging to a scope, sorted, or nil for a scope that does not exist.
//
// The slice is the table's own and must not be modified by a caller.
func (scopes *AlignedScopes) MembersOf(scope ScopeId) []static_single_assignment.IdentifierId {
	if scopes == nil || scopes.members == nil {
		return nil
	}
	return scopes.members[scope]
}

// Ids returns every scope this pass saw, in the input table's own order.
func (scopes *AlignedScopes) Ids() []ScopeId {
	if scopes == nil {
		return nil
	}
	return scopes.order
}

// Length reports how many scopes exist, for measurement.
func (scopes *AlignedScopes) Length() int {
	if scopes == nil {
		return 0
	}
	return len(scopes.order)
}

// Widened reports how many scopes had at least one endpoint moved, for measurement.
//
// Exposed directly because it is the two-sided key this pass is measured on: a zero here beside a
// closed assertion would mean the assertion was already closed and this pass did nothing.
func (scopes *AlignedScopes) Widened() int {
	if scopes == nil {
		return 0
	}
	return scopes.widened
}

// MemberRanges returns every scoped value's range after alignment, as a table.
//
// This is upstream's aliasing made explicit: every member of a scope reports that scope's widened
// range. See the doc comment on `AlignedScopes` for why this cannot be taken from
// `ReactiveScopes.MemberRanges()` once this pass has run.
func (scopes *AlignedScopes) MemberRanges() *mutation_aliasing.MutableRanges {
	result := &mutation_aliasing.MutableRanges{}
	if scopes == nil {
		return result
	}
	for _, scope := range scopes.order {
		widened := scopes.ranges[scope]
		for _, id := range scopes.members[scope] {
			result.Set(id, widened)
		}
	}
	return result
}
