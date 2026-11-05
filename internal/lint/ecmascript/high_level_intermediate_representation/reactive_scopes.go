package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// ReactiveScopes is the table this pass produces: one scope per equivalence class, plus the
// per-value index into it.
//
// # Why a side table rather than a field on Identifier
//
// Upstream stores the scope on the identifier. This keeps it in a table instead, because a value's
// scope is not a property of the value: it is a property of the class the value belongs to, and there
// are 3,306 classes over 26,962 members. Storing the scope per identifier stores the same answer 8.16
// times on average and invites the two copies to drift. The table below stores each scope once and
// indexes into it, so widening a scope is one write rather than a fan-out, which is exactly the
// operation `ScopeGapPostAlignmentWidening` says a later pass will need.
//
// The cost is one indirection and one thing a caller must hold, the same trade `MutableRanges` made.
// If scopes become permanent infrastructure, `ScopeOf` is the seam that makes moving them onto
// `Identifier` invisible to callers.
//
// The zero value is an empty table and is ready to read: every lookup answers "no scope".
type ReactiveScopes struct {
	byIdentifier map[static_single_assignment.IdentifierId]ScopeId
	ranges       map[ScopeId]mutation_aliasing.MutableRange
	members      map[ScopeId][]static_single_assignment.IdentifierId
	order        []ScopeId
}

// ScopeOf returns the scope a value belongs to, or zero when it has none.
//
// A value that never entered an equivalence class has no scope, which is upstream's `null` and is
// the ordinary answer for most values: 26,962 of a corpus function's values are members and the
// rest are not. Zero rather than a second return because zero is not a valid ScopeId.
func (scopes *ReactiveScopes) ScopeOf(id static_single_assignment.IdentifierId) ScopeId {
	if scopes == nil || scopes.byIdentifier == nil {
		return 0
	}
	return scopes.byIdentifier[id]
}

// RangeOf returns a scope's range, or the unset range for a scope that does not exist.
//
// This is the hull of the member ranges, and after this pass it is also every member's own range.
// See the package comment for why the two are equal here and are not guaranteed to stay equal.
func (scopes *ReactiveScopes) RangeOf(scope ScopeId) mutation_aliasing.MutableRange {
	if scopes == nil || scopes.ranges == nil {
		return mutation_aliasing.MutableRange{}
	}
	return scopes.ranges[scope]
}

// MembersOf returns the values belonging to a scope, sorted, or nil for a scope that does not exist.
//
// The slice is the table's own and must not be modified by a caller. Sorted because
// `DisjointSet.Sets` sorts, and the order is load-bearing for determinism rather than cosmetic.
func (scopes *ReactiveScopes) MembersOf(scope ScopeId) []static_single_assignment.IdentifierId {
	if scopes == nil || scopes.members == nil {
		return nil
	}
	return scopes.members[scope]
}

// Ids returns every scope this pass created, in assignment order.
//
// Assignment order is `DisjointSet.Sets()` order, which is sorted by first member. Returned as a
// slice rather than by ranging a map so a caller iterating scopes gets the same sequence every run.
func (scopes *ReactiveScopes) Ids() []ScopeId {
	if scopes == nil {
		return nil
	}
	return scopes.order
}

// Length reports how many scopes exist, for measurement.
func (scopes *ReactiveScopes) Length() int {
	if scopes == nil {
		return 0
	}
	return len(scopes.order)
}

// MemberRanges returns every scoped value's range after entanglement, as a table.
//
// This is upstream's `identifier.mutableRange = scope.range` (line 32261) expressed as a derived
// view rather than as a write back into the input. Every member of a scope reports that scope's
// hull; a value with no scope is absent, and `Get` answers the unset range for it exactly as
// `MutableRanges` does.
//
// Kept separate from the input table on purpose. Upstream overwrites the value's own range and the
// pre-entanglement range is unrecoverable afterwards; here both are available, which costs one map
// and lets a consumer ask which of the two it wants. Nothing upstream needs the old value, so this
// is a capability rather than a divergence in what the pass decides.
func (scopes *ReactiveScopes) MemberRanges() *mutation_aliasing.MutableRanges {
	result := &mutation_aliasing.MutableRanges{}
	if scopes == nil {
		return result
	}
	for _, scope := range scopes.order {
		hull := scopes.ranges[scope]
		for _, id := range scopes.members[scope] {
			result.Set(id, hull)
		}
	}
	return result
}
