package high_level_intermediate_representation

import "github.com/system-inc/cohere/static_single_assignment"

// ScopeDependencies is the table this pass produces: the three scope fields, keyed by scope.
//
// A side table for the reason `ReactiveScopes` is one: the scope lives in a table rather than on the
// identifier, so its outputs live beside it. The zero value is empty and ready to read.
type ScopeDependencies struct {
	// temporaries maps a temporary to the access path it holds, kept so the memoization validator
	// can normalize an inferred dependency into the shape a source dependency is written in.
	//
	// Upstream's `ValidatePreservedManualMemoization` holds its own copy of this map, built by the
	// same walk that collects dependencies. Recording it here rather than rebuilding it is what
	// keeps the two answers identical: a second `collectTemporaries` over a tree three passes later
	// would see a rewritten graph and resolve some paths differently.
	temporaries temporaries

	dependencies map[ScopeId][]ReactiveScopeDependency
	declarations map[ScopeId][]static_single_assignment.IdentifierId
	// declarationOrigin names the scope a declared value actually originated in.
	//
	// `declarations` records a value into every enclosing scope on its stack, which is upstream's
	// behaviour and is what lets an outer scope see a value produced within it. The key therefore
	// says which scope holds the declaration, not which one produced it, and those differ whenever
	// a value bubbles up.
	//
	// Upstream keeps the origin in the map's value: `scope.declarations.set(id, {identifier, scope:
	// originalDeclaration.scope.value})`. This table dropped it on the reasoning that "the scope is
	// the map key, so the pair would store it twice" -- which is true for the key and false for the
	// origin. `pruneUnusedScopes` is the pass that needs the difference: its `hasOwnDeclaration`
	// prunes a scope whose declarations all bubbled up from inner ones, and without the origin that
	// question cannot be asked at all.
	declarationOrigin map[static_single_assignment.IdentifierId]ScopeId
	reassignments     map[ScopeId][]static_single_assignment.IdentifierId
	order             []ScopeId
	// conflicts counts hoistable entries that disagreed about an access type. Upstream raises an
	// invariant on these; see `hoistableTreeFor`. Measured at zero on the corpus.
	conflicts int
}

// DependenciesOf returns a scope's inputs, or nil for a scope with none.
//
// The slice is the table's own and must not be modified by a caller.
func (dependencies *ScopeDependencies) DependenciesOf(scope ScopeId) []ReactiveScopeDependency {
	if dependencies == nil || dependencies.dependencies == nil {
		return nil
	}
	return dependencies.dependencies[scope]
}

// DeclarationsOf returns the values a scope produces that are used outside it.
//
// Upstream stores `(IdentifierId, ReactiveScopeDeclaration)` pairs where the second names the
// declaring scope. The scope is the map key here, so the pair would store it twice.
func (dependencies *ScopeDependencies) DeclarationsOf(scope ScopeId) []static_single_assignment.IdentifierId {
	if dependencies == nil || dependencies.declarations == nil {
		return nil
	}
	return dependencies.declarations[scope]
}

// OriginOf returns the scope a declared value was produced in, and whether it is known.
//
// Distinct from the key of `DeclarationsOf`, which names a scope that holds the declaration and may
// be an enclosing one. `pruneUnusedScopes` prunes a scope whose declarations all came from within
// it, and that is the only question this answers.
func (dependencies *ScopeDependencies) OriginOf(identifier static_single_assignment.IdentifierId) (ScopeId, bool) {
	if dependencies == nil || dependencies.declarationOrigin == nil {
		return 0, false
	}
	origin, found := dependencies.declarationOrigin[identifier]
	return origin, found
}

// PruneDeclarationsLastUsedBefore drops a scope's declarations that are not read after it ends.
//
// Upstream's `updateScopeDeclarations`, which runs after a merge widens a scope's range: a value
// declared by the survivor but last read inside the range it absorbed is no longer an output of the
// merged scope, because nothing outside reads it.
//
// # Why this is a mutator on a table that is otherwise read-only
//
// Everything else here is written once by the collector and read thereafter. This is the exception
// because the scope range it depends on is not final when the collector runs -- the merge widens it
// afterwards -- so the pruning cannot happen at collection time. Upstream has the same shape and the
// same reason.
//
// # When this matters, since it did not for the first consumer
//
// A wider declaration set is safe wherever declarations are read to permit something:
// `pruneUnusedScopes` keeps a scope that declares a value of its own, so extra declarations keep
// extra scopes and cost only granularity.
//
// It is not safe where they are read to deny. `pruneAlwaysInvalidatingScopes` uses declarations as a
// propagation channel -- every declaration naming an always-invalidating value adds that value to
// the unmemoized set, which prunes further scopes downstream -- so a declaration upstream would have
// dropped prunes a scope upstream keeps, and the rule reads the pruned set.
//
// A declaration with no recorded last usage is kept, which is the conservative direction: a value
// this pass knows nothing about is not a value proven dead.
func (dependencies *ScopeDependencies) PruneDeclarationsLastUsedBefore(
	scope ScopeId,
	end static_single_assignment.EvaluationOrder,
	usage *LastUsage,
	function *Function,
) int {
	if dependencies == nil || dependencies.declarations == nil || usage == nil || function == nil {
		return 0
	}
	held := dependencies.declarations[scope]
	if len(held) == 0 {
		return 0
	}

	kept := make([]static_single_assignment.IdentifierId, 0, len(held))
	for _, declared := range held {
		lastUsedAt, found := usage.LastUsedAt(declarationOf(function, declared))
		if found && lastUsedAt < end {
			continue
		}
		kept = append(kept, declared)
	}
	removed := len(held) - len(kept)
	if removed > 0 {
		dependencies.declarations[scope] = kept
	}
	return removed
}

// PruneNonReactiveDependenciesOf drops a scope's dependencies whose root is not reactive.
//
// Upstream's deletion inside `pruneNonReactiveDependencies`. A mutator on an otherwise read-only
// table for the same reason `PruneDeclarationsLastUsedBefore` is one: the fact it depends on --
// whether a value can change between renders -- is not known when the collector runs.
//
// This is the only pass in phase 6 that changes a value the rule reads directly. The rule compares
// dependency sets, so a set left wider than upstream's is a set that will not correspond.
func (dependencies *ScopeDependencies) PruneNonReactiveDependenciesOf(scope ScopeId,
	reactive map[static_single_assignment.IdentifierId]bool) int {
	if dependencies == nil || dependencies.dependencies == nil || reactive == nil {
		return 0
	}
	held := dependencies.dependencies[scope]
	if len(held) == 0 {
		return 0
	}

	kept := make([]ReactiveScopeDependency, 0, len(held))
	for _, dependency := range held {
		if reactive[dependency.Identifier] {
			kept = append(kept, dependency)
		}
	}
	removed := len(held) - len(kept)
	if removed > 0 {
		dependencies.dependencies[scope] = kept
	}
	return removed
}

// ReassignmentsOf returns the bindings a scope reassigns.
func (dependencies *ScopeDependencies) ReassignmentsOf(scope ScopeId) []static_single_assignment.IdentifierId {
	if dependencies == nil || dependencies.reassignments == nil {
		return nil
	}
	return dependencies.reassignments[scope]
}

// Ids returns every scope this pass recorded, in the order it closed them.
//
// Returned as a slice rather than by ranging a map so a caller iterating scopes gets the same
// sequence every run. Go map iteration is randomised, and a pass whose output order varies makes a
// cache non-reproducible without ever producing a wrong answer.
func (dependencies *ScopeDependencies) Ids() []ScopeId {
	if dependencies == nil {
		return nil
	}
	return dependencies.order
}

// Length reports how many scopes carry an entry, for measurement.
func (dependencies *ScopeDependencies) Length() int {
	if dependencies == nil {
		return 0
	}
	return len(dependencies.order)
}

// Conflicts reports hoistable entries that disagreed about whether an access was optional.
//
// Upstream raises `CompilerError.invariant('Conflicting access types')` on any of these. Measured at
// zero across 400 corpus files, so this exists to make a future non-zero visible rather than to
// describe current behaviour.
func (dependencies *ScopeDependencies) Conflicts() int {
	if dependencies == nil {
		return 0
	}
	return dependencies.conflicts
}

// NormalizeInferredDependency rewrites an inferred dependency into the shape a source one is in.
//
// Upstream's normalization step at the top of `validateInferredDep`
// (`ValidatePreservedManualMemoization.ts:236-264`). An inferred dependency is rooted at whatever
// identifier the collector recorded, which for `props.a.b` is the temporary holding `props.a`. A
// source dependency is rooted at what the developer wrote. Comparing the two without this step
// compares a temporary against a name and reports every dependency as a root difference.
//
// When the root is a known temporary, its path is prepended to the inferred path, which is what
// reassembles `t = props.a; t.b` into `props.a.b`. Otherwise the dependency is already rooted at a
// real binding and only needs rewrapping.
//
// Returns false when the root is neither a known temporary nor a named binding. Upstream raises an
// invariant there; this declines instead, because an unnamed root that is not in the map cannot be
// compared against anything a developer could have written, and reporting it would be a finding
// about the lowering rather than about the source.
func (dependencies *ScopeDependencies) NormalizeInferredDependency(function *Function,
	dependency ReactiveScopeDependency) (ManualMemoDependency, bool) {
	if dependencies == nil || function == nil {
		return ManualMemoDependency{}, false
	}

	if resolved, found := dependencies.temporaries[dependency.Identifier]; found {
		path := make([]DependencyPathEntry, 0, len(resolved.Path)+len(dependency.Path))
		path = append(path, resolved.Path...)
		path = append(path, dependency.Path...)
		return ManualMemoDependency{
			Root: ManualMemoRoot{Place: Place{Identifier: resolved.Identifier,
				Reactive: resolved.Reactive}},
			Path: path,
		}, true
	}

	if int(dependency.Identifier) >= len(function.Identifiers) {
		return ManualMemoDependency{}, false
	}
	identifier := function.Identifiers[dependency.Identifier]
	if identifier == nil || identifier.Name == "" {
		return ManualMemoDependency{}, false
	}
	path := make([]DependencyPathEntry, len(dependency.Path))
	copy(path, dependency.Path)
	return ManualMemoDependency{
		Root: ManualMemoRoot{Place: Place{Identifier: dependency.Identifier,
			Reactive: dependency.Reactive}},
		Path: path,
	}, true
}
