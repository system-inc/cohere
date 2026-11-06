// Turning scopes with no outputs into ordinary blocks.
//
// This is React's `pruneUnusedScopes` (`ReactiveScopes/PruneUnusedScopes.ts` at
// `react_conformance.UpstreamSha`) plus `updateScopeDeclarations`, which is
// `MergeReactiveScopesThatInvalidateTogether`'s helper and lands here because this pass is what
// makes it load-bearing.
//
// # Why the rule needs this
//
// `validatePreservedManualMemoization` keeps two live sets, `scopes` and `prunedScopes`, and a
// memoized value whose scope was pruned is treated differently from one whose scope survived. This
// pass is what puts ids into the second set, so without it every scope looks live to the rule.
//
// # Pruned is a flag here, not a second statement type
//
// Upstream emits a `pruned-scope` statement distinct from `scope`. This tree carries `Pruned` on
// `ReactiveScopeBlock` instead, which was already declared and always false, with a comment naming
// the four upstream passes that set it. This is the first of those four to exist. The shapes are
// equivalent -- both say "this was a scope and its memoization is discarded" -- and the flag avoids
// a variant every visitor and transform would have to switch on.
package high_level_intermediate_representation

// PruneUnusedScopes converts scopes with no outputs of their own into ordinary blocks.
//
// Returns how many it pruned, so a caller can tell a no-op from an unrun pass.
//
// A scope is pruned when it holds no return statement, reassigns nothing, and either declares
// nothing or declares only values that originated in scopes nested inside it. The last clause is
// upstream's `hasOwnDeclaration` and it is the reason `OriginOf` exists: a declaration recorded
// against a scope may have been produced within it and bubbled up, and a scope whose entire
// declaration set arrived that way has produced nothing itself.
//
// The return-statement condition is upstream's and it is not obvious. A scope containing a `return`
// is kept even with no declarations, because the value being memoized is early-returned from inside
// it -- `propagateEarlyReturns` rewrites that shape later and needs the scope still standing.
func PruneUnusedScopes(tree *ReactiveFunction, dependencies *ScopeDependencies) int {
	if tree == nil || dependencies == nil {
		return 0
	}
	pruner := scopePruner{dependencies: dependencies}
	pruner.walk(tree.Body)
	return pruner.pruned
}
