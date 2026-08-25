// Merging adjacent scopes that invalidate together: the tree-level merge.
//
// This is React's `mergeReactiveScopesThatInvalidateTogether`
// (`ReactiveScopes/MergeReactiveScopesThatInvalidateTogether.ts` at `reactconformance.UpstreamSha`).
//
// # Not the merge this package already has, and the distinction is load-bearing
//
// `merge_scopes.go` is `mergeOverlappingReactiveScopesHIR`, upstream pipeline position 394. It runs
// on the GRAPH and exists as a precondition: it unions scopes whose ranges overlap without nesting,
// so that every scope can become a block. Without it `assertValidBlockNesting` fails outright.
//
// This is pipeline position 482, runs on the TREE, and is an optimisation rather than a
// precondition. It fuses ADJACENT scopes whose invalidation conditions are identical, so that two
// scopes recomputing under exactly the same circumstances become one. Nothing breaks if it does not
// run; the output is merely more granular than upstream's.
//
// Confusing the two is easy and the names encourage it. The first asks "do these ranges overlap",
// the second asks "would these two always recompute together".
//
// # Why the rule needs it, which is the reason this is ported at all
//
// `scope.merged` -- the set of scope ids a surviving scope absorbed -- is written in exactly one
// place in the whole compiler, this pass, and read by `validatePreservedManualMemoization` to fold
// absorbed ids into the scope set it tracks. Producer and consumer with no codegen between them.
//
// That is the opposite of `promoteUsedTemporaries`, which was declined: the only consumer of what
// that pass wrote was the renamer.
package hir

import (
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
)

// LastUsage records the last instruction at which each declaration is read.
//
// Upstream's `FindLastUsageVisitor`. Keyed by `DeclarationId` rather than `IdentifierId`, which is
// upstream's choice and carries its own TODO saying so: declaration-keying is what makes the output
// compatible, and it is deliberately imprecise where a scope defines a variable whose version is
// never read and is overwritten later. Do not "fix" this to `IdentifierId` -- upstream's note says
// that is correct only once the pass leaves single-assignment form behind, which it has not.
type LastUsage struct {
	byDeclaration map[DeclarationId]EvaluationOrder
}

// LastUsedAt returns the last order at which a declaration was read, and whether it was read at all.
//
// The second return is not decoration. Upstream indexes its map with a non-null assertion and would
// throw on a miss; a linter cannot, and the two callers treat a miss differently -- one skips the
// declaration, the other declines the merge.
func (l *LastUsage) LastUsedAt(declaration DeclarationId) (EvaluationOrder, bool) {
	if l == nil || l.byDeclaration == nil {
		return 0, false
	}
	order, found := l.byDeclaration[declaration]
	return order, found
}

// Len reports how many declarations were seen, for measurement.
func (l *LastUsage) Len() int {
	if l == nil {
		return 0
	}
	return len(l.byDeclaration)
}

// FindLastUsage records, for every declaration, the highest evaluation order at which it is read.
//
// Walks the tree rather than the graph, because the merge that consumes it operates on the tree and
// the two do not agree: a value block's instructions are nested inside a value here and are
// ordinary instructions there.
// The graph is taken alongside the tree because a `Place` here names an `IdentifierId` and this
// pass keys on `DeclarationId`; the mapping between them lives on the graph's identifier table.
func FindLastUsage(tree *ReactiveFunction, function *Function) *LastUsage {
	usage := &LastUsage{byDeclaration: map[DeclarationId]EvaluationOrder{}}
	if tree == nil || function == nil {
		return usage
	}

	VisitReactiveFunction(tree, ReactiveVisitor{
		Place: func(order EvaluationOrder, place Place, role PlaceRole) {
			// `declarationOf` answers zero for an identifier missing from the table, which groups
			// every such value together. That is the shelf helper's documented behaviour and it is
			// acceptable here for the same reason it is there: the consumers compare a declaration
			// against a scope's own declarations, which are written from real places.
			declaration := declarationOf(function, place.Identifier)
			if previous, seen := usage.byDeclaration[declaration]; !seen || order > previous {
				usage.byDeclaration[declaration] = order
			}
		},
	})
	return usage
}

// AreEqualDependencies reports whether two dependency sets name the same inputs.
//
// Upstream's `areEqualDependencies`. Set equality rather than slice equality: order is an artifact
// of collection, so two scopes depending on the same things in a different sequence must compare
// equal or the merge would depend on traversal order.
//
// Path comparison goes through `equalPaths`, which compares `Optional` as well as `Property`. That
// matters here for the same reason it matters there: `a?.b` and `a.b` are different dependencies,
// and collapsing them would merge two scopes that invalidate under different conditions.
func AreEqualDependencies(a, b []ReactiveScopeDependency) bool {
	if len(a) != len(b) {
		return false
	}
	for _, left := range a {
		found := false
		for _, right := range b {
			if left.Identifier == right.Identifier && equalPaths(left.Path, right.Path) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// AreLValuesLastUsedByScope reports whether every named value is read for the last time inside scope.
//
// Upstream's `areLValuesLastUsedByScope`. A value still read after the scope ends cannot have its
// scope merged forward, because the merged scope would extend past the point where that value must
// already be settled.
//
// A declaration with no recorded usage answers FALSE, which is the conservative direction and
// differs from upstream only in that upstream would throw. A value never read is not a value proven
// safe to merge; it is a value this pass has no information about.
func AreLValuesLastUsedByScope(scopeEnd EvaluationOrder, lvalues []DeclarationId,
	usage *LastUsage) bool {
	for _, lvalue := range lvalues {
		lastUsedAt, found := usage.LastUsedAt(lvalue)
		if !found || lastUsedAt >= scopeEnd {
			return false
		}
	}
	return true
}

// ScopeIsEligibleForMerging reports whether a scope may be fused with the ones after it.
//
// Upstream's `scopeIsEligibleForMerging`, and its comment is the whole reasoning: merging is only
// safe when the scope's output is guaranteed to change whenever its input changes. When the output
// may not change, keeping the scopes separate lets the later one compare its input and skip the
// update -- so merging there would make the program recompute more, not less.
//
// A scope with no dependencies is the special case and it is eligible: its output can never change,
// so there is nothing for a later scope to compare against and nothing lost by fusing.
//
// Otherwise the scope is eligible only if it declares a value of an always-invalidating type. Those
// are the values whose identity is fresh on every evaluation, so their scope really does invalidate
// whenever its inputs do.
func ScopeIsEligibleForMerging(function *Function, scope ScopeId, dependencies *ScopeDependencies,
	typeChecker *shimchecker.Checker) bool {
	if dependencies == nil {
		return false
	}
	if len(dependencies.DependenciesOf(scope)) == 0 {
		return true
	}
	for _, declaration := range dependencies.DeclarationsOf(scope) {
		if IsAlwaysInvalidatingType(function, declaration, typeChecker) {
			return true
		}
	}
	return false
}

// CanMergeScopes reports whether two adjacent scopes always invalidate together.
//
// Upstream's `canMergeScopes`, three conditions in order.
//
// # One: neither scope reassigns
//
// A reassignment makes a scope's output depend on control flow rather than on its dependencies
// alone, so two such scopes cannot be shown to invalidate together. Worth knowing before trusting
// corpus coverage here: only 2 of 1,576 corpus scopes carry a reassignment, so this arm is real but
// rare and needs its own fixture rather than the corpus to exercise it.
//
// # Two: identical dependencies
//
// Two scopes reading exactly the same inputs recompute under exactly the same conditions, so one
// scope does the work of both.
//
// # Three: the earlier scope's outputs are the later scope's inputs
//
// The arm with the judgment in it, and upstream's comment explains why it is not simply "the values
// flow from one to the other". A scope's output is not guaranteed to change when its input changes:
// `foo(x)` returning `x < 10` is unchanged as `x` goes from 0 to 1. Fusing on flow alone would make
// the later scope recompute whenever the earlier one's inputs moved, even when its own actual input
// did not.
//
// So the flow must additionally be through values whose type guarantees a fresh identity every
// evaluation, which is what `IsAlwaysInvalidatingType` answers. Upstream also requires those
// dependencies be rooted -- `path.length === 0` -- because a property read off an invalidating
// value is not itself guaranteed to change.
func CanMergeScopes(function *Function, current, next ScopeId, dependencies *ScopeDependencies,
	temporaries map[DeclarationId]DeclarationId, typeChecker *shimchecker.Checker) bool {
	if dependencies == nil {
		return false
	}
	if len(dependencies.ReassignmentsOf(current)) != 0 ||
		len(dependencies.ReassignmentsOf(next)) != 0 {
		return false
	}

	nextDependencies := dependencies.DependenciesOf(next)
	if AreEqualDependencies(dependencies.DependenciesOf(current), nextDependencies) {
		return true
	}

	// The earlier scope's declarations, read as if they were a dependency set, so the comparison
	// against the later scope's inputs is the same one condition two performs. Upstream builds the
	// same synthetic set inline, with `reactive: true` and an empty path.
	asDependencies := make([]ReactiveScopeDependency, 0, len(dependencies.DeclarationsOf(current)))
	for _, declaration := range dependencies.DeclarationsOf(current) {
		asDependencies = append(asDependencies, ReactiveScopeDependency{
			Identifier: declaration,
			Reactive:   true,
		})
	}
	if AreEqualDependencies(asDependencies, nextDependencies) {
		return true
	}

	if len(nextDependencies) == 0 {
		return false
	}
	for _, dependency := range nextDependencies {
		if len(dependency.Path) != 0 {
			return false
		}
		if !IsAlwaysInvalidatingType(function, dependency.Identifier, typeChecker) {
			return false
		}
		if !declaredByOrAliasedFrom(function, dependency.Identifier,
			dependencies.DeclarationsOf(current), temporaries) {
			return false
		}
	}
	return true
}

// declaredByOrAliasedFrom reports whether a dependency comes from one of the given declarations.
//
// Either directly, or through the temporaries map the driver threads along -- upstream resolves a
// `StoreLocal` chain so that a value copied into a temporary is still recognised as the earlier
// scope's output. Without that indirection an ordinary `const b = a` between two scopes would hide
// the flow and silently prevent a merge upstream performs.
func declaredByOrAliasedFrom(function *Function, dependency IdentifierId,
	declarations []IdentifierId, temporaries map[DeclarationId]DeclarationId) bool {
	target := declarationOf(function, dependency)
	aliased, hasAlias := temporaries[target]
	for _, declaration := range declarations {
		candidate := declarationOf(function, declaration)
		if candidate == target {
			return true
		}
		if hasAlias && candidate == aliased {
			return true
		}
	}
	return false
}
