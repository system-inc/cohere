package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
)

// MergedScopes is what this pass produces: the surviving scopes and where the merged ones went.
//
// A scope that was not merged maps to itself and keeps its range. A scope that was merged maps to
// its group's representative and is no longer in `Ids`. This is a derived view rather than a
// rewrite of the input table, for the reason `MemberRanges` records: keeping both available costs
// one map and lets a consumer ask which of the two it wants.
//
// The zero value is an empty result and is ready to read.
type MergedScopes struct {
	ranges  map[ScopeId]mutation_aliasing.MutableRange
	group   map[ScopeId]ScopeId
	members map[ScopeId][]static_single_assignment.IdentifierId
	order   []ScopeId
	unions  int
}

// GroupOf returns the scope a scope was merged into, or the scope itself when it was not merged.
//
// Upstream's `joinedScopes.find(originalScope)` with its `?? originalScope` fallback folded in, so
// a caller does not re-derive it. A scope this pass never saw answers itself, which is the answer
// that lets a caller key a map on the result without first asking whether it merged.
func (scopes *MergedScopes) GroupOf(scope ScopeId) ScopeId {
	if scopes == nil || scopes.group == nil {
		return scope
	}
	if group, ok := scopes.group[scope]; ok {
		return group
	}
	return scope
}

// RangeOf returns a surviving scope's widened range, or the unset range for one that did not survive.
//
// Ask `GroupOf` first when holding a pre-merge id: a merged scope's range lives on its group.
func (scopes *MergedScopes) RangeOf(scope ScopeId) mutation_aliasing.MutableRange {
	if scopes == nil || scopes.ranges == nil {
		return mutation_aliasing.MutableRange{}
	}
	return scopes.ranges[scope]
}

// MembersOf returns every value belonging to a surviving scope, including the values that arrived
// through a merge, sorted.
//
// The slice is the table's own and must not be modified by a caller.
func (scopes *MergedScopes) MembersOf(scope ScopeId) []static_single_assignment.IdentifierId {
	if scopes == nil || scopes.members == nil {
		return nil
	}
	return scopes.members[scope]
}

// Ids returns the surviving scopes, in the input table's own order.
//
// Merged-away scopes are absent. Returned as a slice rather than by ranging a map so a caller gets
// the same sequence every run.
func (scopes *MergedScopes) Ids() []ScopeId {
	if scopes == nil {
		return nil
	}
	return scopes.order
}

// Length reports how many scopes survived, for measurement.
func (scopes *MergedScopes) Length() int {
	if scopes == nil {
		return 0
	}
	return len(scopes.order)
}

// Unions reports how many scopes were merged away, for measurement.
//
// This is `Len` subtracted from the input table's length, exposed directly because it is the number
// the counterfactual in this file's comment is stated in and a test comparing the two spellings
// needs it without recomputing.
func (scopes *MergedScopes) Unions() int {
	if scopes == nil {
		return 0
	}
	return scopes.unions
}
