// Checking that a developer's own memoization survived compilation.
//
// This is React's `validatePreservedManualMemoization`
// (`Validation/ValidatePreservedManualMemoization.ts` at `react_conformance.UpstreamSha`), and it is
// the rule the whole of phase 6 exists to enable.
//
// # What it reports
//
// A developer who wrote `useMemo` or `useCallback` made a claim: this value is stable across renders
// unless these dependencies change. The compiler is free to rewrite that, and normally does it
// better -- but if the rewrite ends up not memoizing the value, the developer's guarantee silently
// disappears. Code that relied on referential stability then breaks in a way nothing else catches.
//
// So the rule compares what was written against what survived, and reports where they disagree.
//
// # Two firing conditions, both about scopes rather than about dependencies
//
// At `StartMemoize`, an operand belonging to a scope that neither survived nor was pruned means the
// dependency may be mutated after the memo block, so the memoization cannot be trusted.
//
// At `FinishMemoize`, a memoized value belonging to a scope that did not survive means the value was
// memoized in source and is not in output. That is the headline case.
//
// Both read `identifier.scope`. Upstream keeps that on the identifier; this tree keeps it in the
// `ReactiveScopes` side table, which answers the same question -- 15,715 of 61,738 corpus
// identifiers carry one.
package high_level_intermediate_representation

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/static_single_assignment"
)

// PreserveManualMemoizationFinding is one disagreement between source and output.
type PreserveManualMemoizationFinding struct {
	// Identifier is the value whose memoization was lost.
	Identifier static_single_assignment.IdentifierId
	// Scope is the scope it belonged to, which did not survive.
	Scope ScopeId
	// Order is where in the function the finding sits, for reporting.
	Order static_single_assignment.EvaluationOrder
	// Kind separates the two conditions, which carry different messages upstream.
	Kind PreserveManualMemoizationKind
}

const (
	// PreserveManualMemoizationDependencyMutable is the `StartMemoize` condition: a dependency
	// belonging to a scope that neither survived nor was pruned, so it may be modified later.
	PreserveManualMemoizationDependencyMutable PreserveManualMemoizationKind = iota
	// PreserveManualMemoizationValueUnmemoized is the `FinishMemoize` condition: a value memoized in
	// source and not in the compilation output.
	PreserveManualMemoizationValueUnmemoized
)

// ValidatePreservedManualMemoization reports where a developer's memoization did not survive.
//
// The scopes that survived are collected by the same walk that checks, which is upstream's shape and
// is load-bearing: a scope is only known to have survived once the walk reaches it, so a memo block
// is checked against the scopes closed before it rather than against every scope in the function.
func ValidatePreservedManualMemoization(tree *ReactiveFunction, function *Function,
	scopes *ReactiveScopes) []PreserveManualMemoizationFinding {
	if tree == nil || function == nil {
		return nil
	}
	return ValidatePreservedManualMemoizationWithDependencies(tree, function, scopes, nil)
}

// ValidatePreservedManualMemoizationWithDependencies adds the third firing condition.
//
// Separate entry point rather than a changed signature, matching `AssignReactiveScopesWithSets` and
// `PruneNonEscapingScopesWithScopes`: the inferred-versus-written comparison needs the collected
// dependency table and the temporaries map inside it, and neither is derivable from a tree.
//
// A caller that passes nil gets the two scope conditions and nothing else, which is what every
// caller had before this existed.
func ValidatePreservedManualMemoizationWithDependencies(tree *ReactiveFunction, function *Function,
	scopes *ReactiveScopes, dependencies *ScopeDependencies) []PreserveManualMemoizationFinding {
	return ValidatePreservedManualMemoizationWithPruned(tree, function, scopes, dependencies, nil)
}

// ValidatePreservedManualMemoizationWithPruned is the same validation, told which scopes the prune
// chain replaced rather than marked.
//
// `PruneNonEscapingScopes` replaces a scope block with its own instructions, so after it runs nothing
// in the tree records that the scope existed. The walk therefore sees neither a live scope block nor
// a pruned one, and the `DependencyMutable` condition -- which asks exactly "scoped, not survived,
// not pruned" -- reaches a fourth state upstream cannot: scoped, and absent.
//
// Upstream reads a `prunedScopes` set its own pass wrote, so it answers the three-part question with
// three states. Passing the set here does the same. A caller with nothing to pass keeps the previous
// behaviour, which is what the exported wrapper above preserves.
func ValidatePreservedManualMemoizationWithPruned(tree *ReactiveFunction, function *Function,
	scopes *ReactiveScopes, dependencies *ScopeDependencies,
	prunedByChain map[ScopeId]bool) []PreserveManualMemoizationFinding {
	if tree == nil || function == nil {
		return nil
	}
	validator := manualMemoValidator{
		function:         function,
		scopes:           scopes,
		liveScopes:       map[ScopeId]bool{},
		prunedScopes:     map[ScopeId]bool{},
		walkedScopes:     map[ScopeId]bool{},
		prunedByChain:    prunedByChain,
		openMemoBlocks:   map[int]bool{},
		dependencies:     dependencies,
		sourceDeps:       map[int][]ManualMemoDependency{},
		declsInMemoBlock: map[static_single_assignment.DeclarationId]bool{},
	}
	validator.walk(tree.Body)
	return validator.findings
}

// AnalyzePreservedManualMemoization runs the reactive-scope pipeline over one function and validates.
//
// Exported so the rule surface does not have to know the pass order. That order is upstream's and is
// load-bearing rather than incidental: the validation reads which scopes survived, which were pruned
// and which absorbed others in a merge, and each of those facts is written by a different pass. A
// caller running them in a different order gets a validation against a half-built world, which
// reports plausibly and is wrong.
//
// Expects `Lower` and `Construct` to have run already, since a caller that has a `*Function` has
// done both by definition.
func AnalyzePreservedManualMemoization(function *Function,
	typeChecker *shimchecker.Checker) []PreserveManualMemoizationFinding {
	if function == nil {
		return nil
	}
	OutlineFunctions(function)
	InferReactive(function, typeChecker)
	DropManualMemoization(function)

	if InlineImmediatelyInvokedFunctionExpressionsIncludingMemoCallbacks(function) > 0 {
		MergeConsecutiveBlocks(function)
	}
	EliminateDeadCode(function)

	ranges := InferMutableRanges(function)
	set := FindDisjointMutableValuesWithRanges(function, ranges)
	scopes := AssignReactiveScopesWithSets(function, ranges, set)
	aligned, merged := AlignThenMergeReactiveScopes(function, scopes)
	identity := MergedScopeIdentity{Aligned: aligned, Merged: merged}
	BuildReactiveScopeTerminals(function, scopes, identity)

	// Upstream's position: immediately after the scope terminals exist and before dependencies are
	// collected. A scope inside a loop is pruned as policy rather than as a failure, and the
	// validator has to be able to tell those apart -- see `flatten_reactive_loops.go`.
	flattenedScopes := FlattenReactiveLoops(function)
	// Then the scopes a hook call sits in, upstream's next pass. A pruned hook scope is no parent to
	// the scopes nested in it, so a memo callback inside one keeps its own scope through the merge.
	for scope := range FlattenScopesWithHooksOrUse(function) {
		if flattenedScopes == nil {
			flattenedScopes = map[ScopeId]bool{}
		}
		flattenedScopes[scope] = true
	}

	dependencies := CollectScopeDependenciesWithHoistable(function, scopes, identity, ranges)

	tree, _ := BuildReactiveFunctionWithFlattenedScopes(function, flattenedScopes)
	if tree == nil {
		return nil
	}

	nonEscaping := PruneNonEscapingScopesWithScopes(tree, function, dependencies, scopes, typeChecker)
	PruneNonReactiveDependencies(tree, function, dependencies)
	PruneUnusedScopes(tree, dependencies)
	MergeReactiveScopesThatInvalidateTogether(tree, function, dependencies, typeChecker)
	PruneAlwaysInvalidatingScopes(tree, function, dependencies)

	// The third firing condition, on. This gate was closed and the reason it was closed is gone.
	//
	// It read: "the comparison itself is built and tested. Its input is not ready:
	// `CollectScopeDependencies` truncates a path to its root wherever the hoistable set is empty,
	// and the hoistable analysis is `DependencyGapNullPropagation`, declined." That analysis landed.
	// `CollectHoistablePropertyLoads` populates the tree, the collector descends into nested
	// functions and into callbacks it can assume are invoked, and 595 dependencies now carry a path
	// against 2,092 that do not -- where the gate was written, 171 did against 1,435.
	//
	// Its own instruction was "turn this on when the hoistable analysis lands, and expect the
	// ungated numbers to be the ones that move." Both happened. The numbers it recorded --
	// golden 15 to 25 for clean 27 to 37 -- are stale by a dozen commits; measured now, the trade
	// is golden 15 to 19 for one additional false positive.
	//
	// That one is `useCallback-alias-property-load-dep.ts`, traced to a scope boundary rather than
	// to this comparison: `const x = propB.x.y` is declared inside our scope 1, so
	// `checkValidDependency` rejects `x` on the rule this tree shares with upstream verbatim
	// (`decl.order < scopeRange.Start`), while upstream's scope begins after that declaration and
	// names `x`. It belongs to the scope work rather than here.
	return ValidatePreservedManualMemoizationWithPruned(tree, function, scopes, dependencies,
		nonEscaping.PrunedScopes)
}

// ForEachFunctionLike calls visit for every outermost function-like node under root.
//
// Exported for the rule surface. Outermost only, which is the same denominator every measurement in
// this package uses: a nested function is reached through `Function.Functions` rather than by
// walking into it, because an IdentifierId names one value in this function and a different value in
// a nested one.
func ForEachFunctionLike(root *ast.Node, visit func(*ast.Node)) {
	if root == nil {
		return
	}
	root.ForEachChild(func(node *ast.Node) bool {
		if ast.IsFunctionLike(node) {
			visit(node)
			return false
		}
		ForEachFunctionLike(node, visit)
		return false
	})
}
