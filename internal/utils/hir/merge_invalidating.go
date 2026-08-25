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
