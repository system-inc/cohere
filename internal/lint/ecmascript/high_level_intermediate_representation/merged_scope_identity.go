package high_level_intermediate_representation

import "github.com/system-inc/cohere/mutation_aliasing"

// MergedScopeIdentity resolves scope identity through an align-then-merge result.
//
// This is what a caller holding the output of `AlignThenMergeReactiveScopes` passes.
type MergedScopeIdentity struct {
	Aligned *AlignedScopes
	Merged  *MergedScopes
}

// GroupOf returns the merged group a pre-merge scope belongs to, or the scope itself when the merge
// left it alone.
func (identity MergedScopeIdentity) GroupOf(scope ScopeId) ScopeId {
	if scope == 0 {
		return 0
	}
	if identity.Merged != nil {
		if group := identity.Merged.GroupOf(scope); group != 0 {
			return group
		}
	}
	return scope
}

// RangeOf returns the surviving scope's range, preferring the merged table and falling back to the
// aligned one for a scope the merge did not touch.
func (identity MergedScopeIdentity) RangeOf(scope ScopeId) mutation_aliasing.MutableRange {
	if identity.Merged != nil {
		if bounds := identity.Merged.RangeOf(scope); bounds.Start != bounds.End {
			return bounds
		}
	}
	if identity.Aligned != nil {
		return identity.Aligned.RangeOf(scope)
	}
	return mutation_aliasing.MutableRange{}
}
