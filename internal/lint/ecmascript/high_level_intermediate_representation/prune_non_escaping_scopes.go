// Pruning scopes whose values never escape, which is the reason the memoization lattice exists.
//
// This is React's `pruneNonEscapingScopes` (`ReactiveScopes/PruneNonEscapingScopes.ts` at
// `react_conformance.UpstreamSha`), assembled from the pieces this package already carries: the
// lattice in `memoization_level.go`, the per-kind classification in `memoization_inputs.go`, and the
// graph and propagation in `memoization_graph.go`.
//
// # What it does, in four phases
//
//  1. Walk the tree once, building a node per declaration: its memoization level, the operands it
//     depends on, and the scopes it belongs to. The same walk collects the escaping roots.
//  2. Roots are values that are returned, or passed as an argument to a hook. A returned value
//     escapes because the caller holds it; a hook argument escapes because React may retain it,
//     which is why the closure handed to `useEffect` is memoized.
//  3. Propagate outward from the roots. See `ComputeMemoized`.
//  4. Replace any scope with no memoized output by its own instructions, dropping the wrapper.
//
// # Why a scope with no memoized output is worth dropping
//
// Memoizing a value nothing holds costs a comparison every render and buys nothing. Upstream's
// worked example is a component building three arrays where only one is returned: without this pass
// all three are memoized, and two of those memoizations can never pay for themselves.
package high_level_intermediate_representation

import (
	shimchecker "github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/system-inc/cohere/static_single_assignment"
)

// PruneNonEscapingScopesResult reports what the pass did, and what it could not decide.
type PruneNonEscapingScopesResult struct {
	// Pruned is how many scopes were replaced by their own instructions.
	Pruned int
	// Declarations is how many declarations the graph held.
	Declarations int
	// EscapingRoots is how many values were found to escape.
	EscapingRoots int
	// Memoized is how many declarations the propagation held.
	Memoized int
	// MarkersPruned is how many `FinishMemoize` markers were marked pruned.
	//
	// Zero unless the caller supplied scopes, since the marker pass needs to ask which scope an
	// identifier belongs to and this IR keeps that in a side table rather than on the identifier.
	MarkersPruned int
	// PrunedScopes is which scopes were replaced by their own instructions.
	//
	// The block is replaced rather than marked, so after this pass nothing in the tree records that
	// the scope existed. Upstream keeps the same structural result and its validator still knows,
	// because it reads a `prunedScopes` set the pass wrote. Returning the set is how a consumer here
	// can ask the same question.
	//
	// Measured on `error.repro-preserve-memoization-inner-destructured-value-mistaken-as-dependency-mutated-dep`:
	// scope 1 holds the `x` a memo block names as a dependency, this pass replaces its block, and the
	// rule then finds a scope that is neither live nor pruned nor walked and declines to judge it --
	// where upstream reports.
	PrunedScopes map[ScopeId]bool
}

// PruneNonEscapingScopes drops scopes whose values are not held by anything.
//
// The checker is used for nothing today and is taken so the signature does not change when
// `MemoizationInputsGapCallSignatures` closes -- that gap needs a type-keyed lookup and the checker
// is what would answer it.
func PruneNonEscapingScopes(tree *ReactiveFunction, function *Function,
	dependencies *ScopeDependencies, typeChecker *shimchecker.Checker) PruneNonEscapingScopesResult {
	if tree == nil || function == nil {
		return PruneNonEscapingScopesResult{}
	}

	collector := memoizationCollector{
		function:     function,
		dependencies: dependencies,
		graph:        NewMemoizationGraph(),
		definitions:  map[static_single_assignment.DeclarationId]static_single_assignment.DeclarationId{},
	}
	for _, parameter := range function.Parameters {
		collector.graph.Declare(declarationOf(function, parameter.Identifier))
	}
	collector.walk(tree.Body)

	memoized := collector.graph.ComputeMemoized()

	return pruneNonEscapingScopesWith(tree, function, dependencies, collector, memoized, nil)
}

// PruneNonEscapingScopesWithScopes is `PruneNonEscapingScopes` plus the memo-marker pass.
//
// Separate entry point rather than a changed signature, matching `AssignReactiveScopesWithSets` and
// `CollectScopeDependenciesWithHoistable`: the marker pass needs to ask which scope an identifier
// belongs to, and this IR answers that from a side table rather than from the identifier, so the
// question is not askable from the arguments the original takes.
//
// A caller that omits the scopes gets the scope pruning and no marker pruning, which is the state
// every caller was in before this existed.
func PruneNonEscapingScopesWithScopes(tree *ReactiveFunction, function *Function,
	dependencies *ScopeDependencies, scopes *ReactiveScopes,
	typeChecker *shimchecker.Checker) PruneNonEscapingScopesResult {
	if tree == nil || function == nil {
		return PruneNonEscapingScopesResult{}
	}

	collector := memoizationCollector{
		function:     function,
		dependencies: dependencies,
		graph:        NewMemoizationGraph(),
		definitions:  map[static_single_assignment.DeclarationId]static_single_assignment.DeclarationId{},
	}
	for _, parameter := range function.Parameters {
		collector.graph.Declare(declarationOf(function, parameter.Identifier))
	}
	collector.walk(tree.Body)

	memoized := collector.graph.ComputeMemoized()

	return pruneNonEscapingScopesWith(tree, function, dependencies, collector, memoized, scopes)
}

func pruneNonEscapingScopesWith(tree *ReactiveFunction, function *Function,
	dependencies *ScopeDependencies, collector memoizationCollector,
	memoized map[static_single_assignment.DeclarationId]bool, scopes *ReactiveScopes) PruneNonEscapingScopesResult {
	prunedScopes := map[ScopeId]bool{}
	pruned := prunePassOverScopes(tree, function, dependencies, memoized, prunedScopes,
		collector.resolve)
	markers := prunePassOverMemoMarkers(tree, function, scopes, prunedScopes)

	return PruneNonEscapingScopesResult{
		PrunedScopes:  prunedScopes,
		Pruned:        pruned,
		Declarations:  collector.graph.Length(),
		EscapingRoots: collector.graph.EscapingCount(),
		Memoized:      len(memoized),
		MarkersPruned: markers,
	}
}

// memoizationLevelOfDefine returns the level upstream assigns to an assignment target.
//
// It is deliberately not `MemoizationLevelOf` of the enclosing value. Upstream's classification
// returns a list of lvalues each carrying its own level, and for five of the seven kinds the target
// and the instruction's own lvalue are given *different* levels -- a `DeclareContext` writes a
// binding at `Memoized` while its temporary is `Unmemoized`. Reusing the value's level would flatten
// that distinction in the direction that holds too much, which costs granularity silently.
//
// The default is `Conditional` rather than `Never`, because every remaining way a place is defined
// is an indirection: it is memoized exactly when what flows into it is. `Never` would terminate the
// propagation at an assignment, which is the failure this function exists to prevent.
func memoizationLevelOfDefine(value ReactiveValue, place Place) MemoizationLevel {
	plain, isPlain := value.(*ReactiveInstructionValue)
	if !isPlain || plain.Value == nil {
		return MemoizationConditional
	}
	switch shape := plain.Value.(type) {
	// A context binding outlives the instruction that wrote it, so it is never pruned.
	case *DeclareContext, *StoreContext:
		return MemoizationMemoized

	// Declared and not held: not comparable with `Object.is`, and left alone unless forced.
	case *DeclareLocal:
		return MemoizationUnmemoized

	// A destructured pattern binds at `Conditional`, except a rest element, which allocates a fresh
	// object or array every evaluation and so must be held on its own account.
	case *Destructure:
		if isRestPlaceOfPattern(shape.LValue, place) {
			return MemoizationMemoized
		}
		return MemoizationConditional

	default:
		return MemoizationConditional
	}
}

// isRestPlaceOfPattern reports whether a place is a pattern's rest element.
//
// Upstream's `computePatternLValues` reaches `Memoized` for `Spread` in an array pattern and for a
// non-`ObjectProperty` in an object pattern, which are the same thing this tree spells as `Rest`.
func isRestPlaceOfPattern(pattern Pattern, place Place) bool {
	switch shape := pattern.(type) {
	case *ObjectPattern:
		if shape.Rest != nil && shape.Rest.Identifier == place.Identifier {
			return true
		}
		for _, property := range shape.Properties {
			if isRestPlaceOfPattern(property.Value, place) {
				return true
			}
		}
	case *ArrayPattern:
		if shape.Rest != nil && shape.Rest.Identifier == place.Identifier {
			return true
		}
		for _, element := range shape.Elements {
			if element.Value != nil && isRestPlaceOfPattern(element.Value, place) {
				return true
			}
		}
	}
	return false
}

// prunePassOverScopes replaces every scope with no memoized output by its own instructions.
//
// Upstream keeps a scope whose declarations and reassignments are both empty, or which carries an
// early-return value: the memoized value is returned from inside it and `propagateEarlyReturns`
// needs the scope standing. We have no early-return representation, so only the first half applies
// and the second is why `PropagateEarlyReturns` is still open rather than declined.
//
// Mutates: prunedScopes the caller gathers every scope pruned here, so the marker pass after it reads the set
func prunePassOverScopes(
	tree *ReactiveFunction,
	function *Function,
	dependencies *ScopeDependencies,
	memoized map[static_single_assignment.DeclarationId]bool,
	prunedScopes map[ScopeId]bool,
	resolve func(static_single_assignment.DeclarationId) static_single_assignment.DeclarationId,
) int {
	pruned := 0
	TransformReactiveFunction(tree, ReactiveTransformer{
		Scope: func(scope *ReactiveScopeBlock, traverse func()) ReactiveTransformed {
			traverse()
			// A scope pruned before this pass ran is already a decision, and the marker pass below
			// has to see it.
			//
			// `FlattenReactiveLoops` marks a scope inside a loop as pruned while the reactive tree
			// is being built, which is three passes before this one. Upstream reaches the same state
			// by rewriting the terminal into `PrunedScope`, so its `prune_non_escaping_scopes` never
			// sees such a scope as a live one to reason about. Here the block is still a
			// `ReactiveScopeBlock` carrying `Pruned`, so the equivalent is to fold it into the set
			// this pass is accumulating. Without this the marker pass computes `every declaration is
			// in a pruned scope` against a set missing the loop-flattened ones, leaves the
			// `FinishMemoize` unflagged, and the validator reports a memoization the compiler
			// declined on purpose.
			if scope.Pruned {
				if prunedScopes != nil {
					prunedScopes[scope.Scope] = true
				}
				return KeepStatement()
			}
			if dependencies == nil {
				return KeepStatement()
			}
			declarations := dependencies.DeclarationsOf(scope.Scope)
			reassignments := dependencies.ReassignmentsOf(scope.Scope)
			if len(declarations) == 0 && len(reassignments) == 0 {
				return KeepStatement()
			}
			// Resolved through the same `LoadLocal` indirection the collector applied when it
			// recorded these declarations, or the lookup asks for an identity the graph never used.
			//
			// Upstream reads `decl.identifier.declarationId` with no indirection because its
			// declarations already carry the resolved identifier: `scope.declarations` stores the
			// `Identifier` object itself, and its `LoadLocal` handling rewrites that object rather
			// than keeping a side table. This tree keeps the side table, so the resolution has to
			// be re-applied at every consumer.
			//
			// Measured on `error.validate-object-values-mutation` with a widened range: the
			// collector records the `values` binding under declaration 1 and this looked it up
			// under declaration 3, so a memoized value read as unmemoized and the scope holding it
			// was pruned.
			for _, declared := range declarations {
				if memoized[resolve(declarationOf(function, declared))] {
					return KeepStatement()
				}
			}
			for _, reassigned := range reassignments {
				if memoized[resolve(declarationOf(function, reassigned))] {
					return KeepStatement()
				}
			}
			pruned++
			if prunedScopes != nil {
				prunedScopes[scope.Scope] = true
			}
			return ReplaceStatements(scope.Instructions)
		},
	})
	return pruned
}

// prunePassOverMemoMarkers marks a `FinishMemoize` whose value was never memoized.
//
// Upstream's `transformInstruction` in `PruneNonEscapingScopes.ts:1066-1119`, and its doc comment
// names exactly the symptom this closes: "If we pruned the scope for a non-escaping value, we know
// it doesn't need to be memoized. Remove associated `Memoize` instructions so that we don't report
// false positives on 'missing' memoization of these values."
//
// # Why this is a second walk here and one walk upstream
//
// Upstream overrides `transformInstruction` on the same visitor that prunes the scopes, so the two
// interleave in one pass. Our transformer takes hooks rather than being subclassed, and the scope
// hook already runs `traverse()` before deciding, so an instruction hook on the same transformer
// would see a scope's own instructions before the scope decided its fate. Running the marker pass
// after the scope pass reads the finished `prunedScopes` set instead, which is the same information
// without depending on the interleaving.
//
// # The reassignment map, which is the half a shorter port would drop
//
// A `useMemo` that got inlined stores its result into a temporary and then reassigns, so the
// marker's own value carries no scope and the question has to be asked of what was assigned into
// it. Upstream builds that map from two instruction shapes and falls back to the marker's own
// identifier when the map has no entry. Dropping it would leave every inlined memo unmarked, which
// is the shape `prune-nonescaping-useMemo.ts` and its siblings are built from.
func prunePassOverMemoMarkers(tree *ReactiveFunction, function *Function, scopes *ReactiveScopes,
	prunedScopes map[ScopeId]bool) int {
	if scopes == nil {
		return 0
	}
	marked := 0
	reassignments := map[static_single_assignment.DeclarationId][]static_single_assignment.IdentifierId{}

	// scopeless answers upstream's `decl.identifier.scope == null`. Zero is this tree's "no scope"
	// sentinel, standing in for upstream's null.
	scopeless := func(id static_single_assignment.IdentifierId) bool { return scopes.ScopeOf(id) == 0 }

	TransformReactiveFunction(tree, ReactiveTransformer{
		Instruction: func(statement *ReactiveInstructionStatement,
			traverse func()) ReactiveTransformed {
			traverse()
			if statement == nil || statement.Instruction == nil {
				return KeepStatement()
			}
			instruction := statement.Instruction
			plain, isPlain := instruction.Value.(*ReactiveInstructionValue)
			if !isPlain || plain == nil {
				return KeepStatement()
			}

			switch shape := plain.Value.(type) {
			case *StoreLocal:
				// Upstream keys this arm on `lvalue.kind === 'Reassign'`. This IR has no reassign
				// kind on a store, so the same population is selected structurally: a store whose
				// target carries no scope is the inlining temporary upstream is describing.
				if scopeless(shape.LValue.Identifier) {
					target := declarationOf(function, shape.LValue.Identifier)
					reassignments[target] = append(reassignments[target], shape.Value.Identifier)
				}

			case *LoadLocal:
				// The simpler inlining shape: a direct assignment to the original lvalue. Upstream
				// requires the loaded place to have a scope and the lvalue to lack one, which is
				// what distinguishes it from an ordinary read.
				if instruction.LValue != nil && !scopeless(shape.Place.Identifier) &&
					scopeless(instruction.LValue.Identifier) {
					target := declarationOf(function, instruction.LValue.Identifier)
					reassignments[target] = append(reassignments[target], shape.Place.Identifier)
				}

			case *FinishMemoize:
				if shape.Pruned {
					return KeepStatement()
				}
				declarations := []static_single_assignment.IdentifierId{shape.Value.Identifier}
				if scopeless(shape.Value.Identifier) {
					if inlined, found :=
						reassignments[declarationOf(function, shape.Value.Identifier)]; found {
						declarations = inlined
					}
				}
				// Upstream's `every`: a marker is pruned when every declaration it could name is
				// either scopeless or in a scope this pass just pruned. One surviving memoized
				// declaration is enough to keep the claim alive.
				all := true
				for _, declaration := range declarations {
					scope := scopes.ScopeOf(declaration)
					if scope != 0 && !prunedScopes[scope] {
						all = false
						break
					}
				}
				if all {
					shape.Pruned = true
					marked++
				}
			}
			return KeepStatement()
		},
	})
	return marked
}
