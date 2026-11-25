// Scope dependencies: what each reactive scope reads from outside itself.
//
// This is React's `propagateScopeDependenciesHIR` (development bundle line 47327) run over this
// graph. oxc transcribes it at
// `oxc_react_compiler/src/react_compiler_inference/propagate_scope_dependencies_hir.rs`; where the
// two disagree React wins, and each disagreement is recorded at the line that resolves it.
//
// # What this pass computes, and what a "dependency" actually is
//
// A reactive scope memoizes a group of values together. To know when to recompute them, the scope
// needs its inputs: the values it reads that were produced before it began. Those inputs are
// dependencies, and a dependency is not an identifier -- it is an access PATH rooted at one.
//
//	ReactiveScopeDependency { Identifier, Reactive, Path []DependencyPathEntry }
//	DependencyPathEntry     { Property string, Optional bool }
//
// So `props.a.b` is one dependency: root `props`, path `[a, b]`. This matters because a scope
// reading only `props.a.b` should not invalidate when `props.c` changes, and because the dedup
// predicate compares the whole path. Upstream's `areEqualPaths` (bundle line 47352) compares each
// entry's property and its `Optional` flag, so `a?.b` and `a.b` are different dependencies. That
// asymmetry is reproduced here rather than simplified; see `DependencyPathEntry.Optional`.
//
// # The three fields this pass writes, and why they are all one pass
//
// `ReactiveScope` has eight fields. `scopes.go` filled `id`, `range` and `loc` and stopped, naming
// `dependencies`, `declarations` and `reassignments` as belonging here. That boundary was checked
// mechanically before this file was written rather than taken on trust: every writer of the three
// fields across all of oxc was enumerated, and the initial producer of each is this pass alone
// (`propagate_scope_dependencies_hir.rs` lines 111, 1812 and 1857). Every other write is a later
// `prune_*` or `merge_*` pass mutating what this one created, and none of those exists here. The one
// exception found was `propagate_early_returns.rs:199`, a second `declarations` producer, which is
// the pass `scopes.go` had already assigned elsewhere.
//
// # Its input is the graph, not the scope table, which is why three stages had to land first
//
// The single most surprising thing about this pass is that it never reads a scope side table to
// find its subjects. It recovers scopes by walking blocks for a `Scope` terminal, through
// `ScopeBlockTraversal` (bundle 20758). React's `keyByScopeId` (46602) is six lines of
// `if (block.terminal.kind === 'scope')`.
//
// Measured before `BuildReactiveScopeTerminals` existed, this graph held 3,306 scopes in the side
// table and 0 reachable from the CFG. That zero was the denominator, not a gap: `collectDependencies`
// keys its output map on scope ids obtained only from that traversal, and `visitDependency` and
// `visitReassignment` are both gated on the scope stack being non-empty. A port written against the
// side table would have terminated, passed every fixture, and produced nothing -- the characteristic
// failure of this pass is an empty output that looks plausible. Three stages (alignment, merging,
// terminal construction) closed it, and the count is now 2,874 reachable.
//
// The lesson generalised into the instrument this package now uses: a writer-check confirms the
// output field belongs to this file and is silent on whether the file's inputs exist. Both levels
// have to be checked, and the second level is "is the type ever CONSTRUCTED", not merely declared.
// `Optional` used to be the standing example; optional-chain lowering now constructs it and its
// sidemap is an input to this pass.
//
// # Divergence from React: oxc sorts a collection React does not
//
// `ReactiveScopeDependencyTreeHIR::new` in oxc sorts its hoistable objects so optional-first entries
// precede non-optional ones, with a comment claiming this "matches the TS behavior". React does no
// such sort (bundle 46967): it iterates `hoistableObjects` in insertion order and raises a hard
// `CompilerError.invariant('Conflicting access types')` when two entries disagree, where oxc
// silently takes first-wins via `or_insert_with`.
//
// React is the truth, so no sort is performed here. The divergence is recorded rather than split
// because the conflict behavior is already reproduced directly: inputs retain insertion order and
// `ScopeDependencies.Conflicts` exposes disagreements rather than silently choosing the first.
//
// # Termination is a single ordered walk, with no fixpoint anywhere
//
// There is no worklist and no iteration to convergence. `CollectScopeDependencies` makes one pass
// over the blocks in order, maintaining a stack of active scopes. Every block is visited once, every
// instruction once, and every operand once. The dependency tree is built by insertion and read once
// by `deriveMinimalDependencies`, whose recursion is over a tree whose depth is the longest property
// path and whose nodes are finite and fixed before the walk begins.
//
// This is deliberately narrower than the passes that prepare its inputs. Non-null propagation is a
// separate bounded dataflow pass and optional-chain recovery is a separate structural traversal;
// both finish before this collector receives their sidemaps. `TestDependencyCollectionIsASinglePass`
// pins the walk here, not those producers.
//
// # The distribution that proves these dependencies are real
//
// A count cannot separate a working pass from one that collects nothing, so the headline is a
// cross-tabulation of dependencies per scope against scope member count, over 400 corpus files and
// 677 outermost functions. The control cell is scopes carrying zero dependencies: a pass that
// collected nothing would put every scope there, and a pass that over-collected would empty it.
//
// See `TestDependencyDistributionIsReal`, which pins the shape rather than the corpus numbers.
package high_level_intermediate_representation

import (
	"github.com/system-inc/cohere/mutation_aliasing"
	"github.com/system-inc/cohere/static_single_assignment"
	"strconv"
	"strings"
)

// DependencyPathEntry is one step in an access path: the `.b` in `props.a.b`.
//
// `Optional` records that this step came from `?.` rather than `.`. It is part of the entry's
// identity rather than a decoration: upstream's `areEqualPaths` compares it, so `a?.b` and `a.b` are
// two different dependencies and both can be present on one scope. Reproduced rather than collapsed
// because collapsing them would silently merge a null-guarded read with an unguarded one.
type DependencyPathEntry struct {
	Property string
	Optional bool
}

// ReactiveScopeDependency is one input to a reactive scope: an access path rooted at a value.
//
// The root is an `IdentifierId` and the path may be empty, which is the ordinary case for a bare
// value used directly. `Reactive` is carried from the `Place` the dependency was read through, and
// is upstream's `reactive` field; it is not consulted by this pass and exists for the pruning pass
// that would consume it (`prune_non_reactive_dependencies.rs:230` retains on exactly this).
type ReactiveScopeDependency struct {
	Identifier static_single_assignment.IdentifierId
	Reactive   bool
	Path       []DependencyPathEntry
}

// equalPaths reports whether two access paths are identical, upstream's `areEqualPaths`.
//
// Compares `Optional` as well as `Property`, which is what makes `a?.b` and `a.b` distinct. A plain
// property-name comparison passes every fixture built from unguarded accesses and silently merges
// the guarded ones, so the flag is compared here rather than at the call site.
func equalPaths(left, right []DependencyPathEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Property != right[index].Property || left[index].Optional != right[index].Optional {
			return false
		}
	}
	return true
}

// DependencyGap names a dependency rule this pass cannot apply, for a caller that needs to know.
//
// Modelled on `RangeGap`, `ScopeGap` and `ScopeTerminalsGap` deliberately and for the same reason: a
// gap answerable through the API is one a consumer can decline on, while a gap living only in a
// comment is one the next reader inherits by accident.
type DependencyGap uint8

const (
	// DependencyGapOptionalChains is the residual optional-chain surface not yet lowered into the
	// control-flow shape this pass consumes.
	//
	// Dot-property chains are implemented: lowering emits Optional/Branch/value blocks and
	// `CollectOptionalChainSidemap` reconstructs their paths, processed instructions, test terminals
	// and hoistable objects. Computed optional links and optional calls still use the older
	// instruction flags/call lowering, so they do not enter that protocol. A consumer requiring full
	// optional-chain coverage must still decline on this gap until those two forms join the same CFG.
	DependencyGapOptionalChains DependencyGap = iota

	// DependencyGapTypeExclusions is upstream's two type-based dependency filters, not applied.
	//
	// `checkValidDependency` rejects two kinds of value outright: a ref's `.current` (via
	// `isRefValueType`) and an object method (`Type::ObjectMethod`). Both are exclusions, so their
	// absence can only add dependencies, never drop one.
	//
	// This IR carries no type (measured when it still had an always-nil type field: 0 of 110,265
	// identifiers over the corpus carried one). So neither predicate is expressible. The
	// exposure was measured rather than estimated: 7 `ObjectMethod` instructions and 3 `.current`
	// property loads across 4,766 loads in 677 functions. The object-method half is recoverable
	// structurally -- an `ObjectMethod` instruction is identifiable without types -- and is applied
	// here; only the ref half is genuinely absent, and it is three accesses.
	DependencyGapTypeExclusions
)

// DependencyGaps are the rules CollectScopeDependencies does not apply. See DependencyGap.
//
// Returned as a value rather than documented alone so a test can assert on it, which makes closing a
// gap a visible event rather than a silent improvement.
func DependencyGaps() []DependencyGap {
	return []DependencyGap{
		DependencyGapOptionalChains,
		DependencyGapTypeExclusions,
	}
}

// ---------------------------------------------------------------------------
// The dependency tree, and the minimal set it derives
// ---------------------------------------------------------------------------

// propertyAccessType is how a node in the dependency tree was reached.
//
// Two independent bits packed into upstream's four-variant enum: whether the access was guarded by
// `?.` (optional) or not (unconditional), and whether the node is itself a dependency or merely a
// waypoint on the path to one. `mergeAccess` below is what combines two readings of the same node.
type propertyAccessType uint8

const (
	optionalAccess propertyAccessType = iota
	unconditionalAccess
	optionalDependency
	unconditionalDependency
)

func isOptionalAccess(access propertyAccessType) bool {
	return access == optionalAccess || access == optionalDependency
}

func isDependencyAccess(access propertyAccessType) bool {
	return access == optionalDependency || access == unconditionalDependency
}

// mergeAccess combines two readings of one tree node, upstream's `merge`.
//
// The two bits combine differently and that asymmetry is the point. "Dependency" is a union: reached
// as a dependency on either path, it is one. "Unconditional" is an intersection expressed by De
// Morgan -- the result is optional only when both readings were optional, so a value read once
// guarded and once unguarded is unguarded, because the unguarded read already happened.
func mergeAccess(left, right propertyAccessType) propertyAccessType {
	unconditional := !(isOptionalAccess(left) && isOptionalAccess(right))
	dependency := isDependencyAccess(left) || isDependencyAccess(right)
	switch {
	case unconditional && dependency:
		return unconditionalDependency
	case unconditional:
		return unconditionalAccess
	case dependency:
		return optionalDependency
	default:
		return optionalAccess
	}
}

func newDependencyNode(access propertyAccessType, reactive bool) *dependencyNode {
	return &dependencyNode{properties: map[string]*dependencyNode{}, access: access, reactive: reactive}
}

// hoistableNode is one value in the tree of accesses proven safe to hoist out of a scope.
//
// This was empty when the type was written, and the comment saying so outlived the fact by several
// changes. `CollectHoistablePropertyLoads` in `hoistable.go` populates it, called from
// `CollectScopeDependenciesWithHoistable`, so a path descends here wherever that analysis proved
// the object non-null. `CollectScopeDependencies` -- the spelling that passes nil -- is the one
// that still sees an empty set, and truncates every path to its root as a result.
type hoistableNode struct {
	properties map[string]*hoistableNode
	// nonNull is upstream's `HoistableAccessType::NonNull`, meaning this access is known not to
	// throw and may therefore be read before the scope runs.
	nonNull bool
}

// newDependencyTree builds the tree, seeding it with the accesses proven hoistable.
//
// # Divergence from oxc, recorded at the line that resolves it
//
// oxc sorts `hoistable_objects` here so that optional-first entries precede non-optional ones,
// commenting that this "matches the TS behavior". It does not. React iterates the collection in
// insertion order with no sort (bundle 46967) and raises `CompilerError.invariant('Conflicting
// access types')` when two entries disagree about a property, where oxc silently keeps the first.
//
// React is the truth, so there is no sort. The divergence was unobservable because `hoistable` was
// always empty, and that is no longer the state: `CollectHoistablePropertyLoads` is built and
// supplies real entries. Measured on `useMemo-alias-property-load-dep.ts`, the tree seeds three
// roots, one carrying a property and marked non-null. So ordering and conflicts are now reachable
// and this note is a live caveat rather than a dormant one.
func newDependencyTree(hoistable map[static_single_assignment.IdentifierId]*hoistableNode) *dependencyTree {
	seeded := hoistable
	if seeded == nil {
		seeded = map[static_single_assignment.IdentifierId]*hoistableNode{}
	}
	return &dependencyTree{hoistable: seeded, roots: map[static_single_assignment.IdentifierId]*dependencyNode{}}
}

// collectMinimalInSubtree is upstream's `collectMinimalDependenciesInSubtree`.
//
// Recursion depth is the longest access path, and the tree is finite and fully built before this
// runs, so this terminates without any convergence argument. See the package comment on termination.
//
// Mutates: results the walk appends every minimal path it finds, so one slice gathers a whole tree
func collectMinimalInSubtree(node *dependencyNode, root static_single_assignment.IdentifierId,
	path []DependencyPathEntry, results *[]ReactiveScopeDependency) {
	if isDependencyAccess(node.access) {
		copied := make([]DependencyPathEntry, len(path))
		copy(copied, path)
		*results = append(*results, ReactiveScopeDependency{
			Identifier: root,
			Reactive:   node.reactive,
			Path:       copied,
		})
		return
	}
	for _, key := range node.keys {
		child := node.properties[key]
		collectMinimalInSubtree(child, root, append(path, DependencyPathEntry{
			Property: key,
			Optional: isOptionalAccess(child.access),
		}), results)
	}
}

// ---------------------------------------------------------------------------
// The temporaries sidemap: resolving a value back to the access path that made it
// ---------------------------------------------------------------------------

// collectTemporaries builds the sidemap, upstream's `collectTemporariesSidemap`.
//
// A value is recorded only when it is not used outside the scope that declares it: a temporary read
// from elsewhere is a real value with its own identity rather than a rename of the thing it loaded.
// `usedOutside` carries that answer, computed by `findTemporariesUsedOutsideDeclaringScope`.
//
// The `LoadLocal` arm requires the destination to be anonymous and the source to be named, which is
// upstream's `lvalue.identifier.name === null && place.identifier.name !== null`. That asymmetry is
// what keeps this a map from temporaries to real values rather than a general alias table.
func collectTemporaries(function *Function, usedOutside map[static_single_assignment.DeclarationId]bool) temporaries {
	result := temporaries{}
	collectTemporariesInto(function, usedOutside, result)
	return result
}

// collectTemporariesInto walks one function, recursing into nested ones.
//
// # Divergence from React: nested functions get their own map here
//
// Upstream threads one `temporaries` map through the recursion into nested functions, keyed by
// `IdentifierId`. That is safe there because ids are unique across the whole compilation. They are
// per-function here -- `FindDisjointMutableValues` records why -- so a shared map would conflate
// values: measured on a two-closure function, 14 collisions across 30 distinct ids.
//
// Nested functions are therefore walked with a fresh map, whose results are discarded. That is
// narrower than upstream, and the narrowing is in the safe direction: a dependency inside a nested
// function resolves to the nested value rather than to a path through the outer one, so a path is
// reported shallower rather than wrong. Recorded because a reader diffing against the bundle will
// see the shared map missing.
//
// Mutates: into the walk records every temporary it resolves, so one map gathers a whole function
func collectTemporariesInto(
	function *Function,
	usedOutside map[static_single_assignment.DeclarationId]bool,
	into temporaries,
) {
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			lvalue := function.Identifiers[instruction.LValue.Identifier]
			if lvalue == nil {
				continue
			}
			if usedOutside[lvalue.Declaration] {
				continue
			}
			switch value := instruction.Value.(type) {
			case *PropertyLoad:
				into[instruction.LValue.Identifier] =
					into.getProperty(value.Object, value.Property, value.Optional)
			case *LoadLocal:
				source := function.Identifiers[value.Place.Identifier]
				if lvalue.Name == "" && source != nil && source.Name != "" {
					into[instruction.LValue.Identifier] = ReactiveScopeDependency{
						Identifier: value.Place.Identifier,
						Reactive:   value.Place.Reactive,
					}
				}
			case *LoadContext:
				source := function.Identifiers[value.Place.Identifier]
				if lvalue.Name == "" && source != nil && source.Name != "" {
					into[instruction.LValue.Identifier] = ReactiveScopeDependency{
						Identifier: value.Place.Identifier,
						Reactive:   value.Place.Reactive,
					}
				}
			}
		}
	}
	// See this function's comment: a fresh map per nested function, because IdentifierIds are
	// per-function here and a shared one would conflate values across the boundary.
	for _, nested := range function.Functions {
		if nested != nil {
			collectTemporariesInto(nested, usedOutside, temporaries{})
		}
	}
}

// findTemporariesUsedOutsideDeclaringScope returns the bindings read outside the scope that made
// them.
//
// Upstream's function of the same name (bundle 47355). A temporary read outside its declaring scope
// cannot be treated as a rename of what it loaded, because the scope boundary means the two are
// separately memoized. Keyed by `DeclarationId` rather than `IdentifierId` so that a binding
// reassigned inside a scope is one subject rather than several.
func findTemporariesUsedOutsideDeclaringScope(function *Function,
	terminals map[static_single_assignment.BlockId]scopeBlockInfo) map[static_single_assignment.DeclarationId]bool {
	declaringScope := map[static_single_assignment.DeclarationId]ScopeId{}
	usedOutside := map[static_single_assignment.DeclarationId]bool{}
	var active []ScopeId

	isActive := func(scope ScopeId) bool {
		for _, candidate := range active {
			if candidate == scope {
				return true
			}
		}
		return false
	}
	handlePlace := func(place Place) {
		identifier := function.Identifiers[place.Identifier]
		if identifier == nil {
			return
		}
		if scope, ok := declaringScope[identifier.Declaration]; ok && !isActive(scope) {
			usedOutside[identifier.Declaration] = true
		}
	}

	// Iterating the slice rather than sorting by id: `Function.Blocks` is held in reverse postorder
	// after lowering, so slice order is execution order and `block.Id` is the addressing key. Using
	// the slice index as a BlockId would be a silent correctness bug, because the two move
	// independently -- see the doc on `BlockId`.
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		if info, ok := terminals[block.Id]; ok {
			if info.begins {
				active = append(active, info.scope)
			} else if len(active) > 0 {
				active = active[:len(active)-1]
			}
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			EachInstructionPlace(instruction, func(place Place, role PlaceRole) {
				if role != PlaceRoleDefine {
					handlePlace(place)
				}
			})
			if len(active) == 0 {
				continue
			}
			switch instruction.Value.(type) {
			case *LoadLocal, *LoadContext, *PropertyLoad:
				if lvalue := function.Identifiers[instruction.LValue.Identifier]; lvalue != nil {
					declaringScope[lvalue.Declaration] = active[len(active)-1]
				}
			}
		}
		EachTerminalPlace(block.Terminal, func(place Place, role PlaceRole) {
			handlePlace(place)
		})
	}
	return usedOutside
}

// ---------------------------------------------------------------------------
// The scope traversal, and the walk that collects
// ---------------------------------------------------------------------------

// scopeBlockInfo marks a block as the start or the end of a reactive scope's body.
//
// Upstream's `ScopeBlockInfo`, a two-variant union of `begin` and `end`. The `pruned` flag it also
// carries is absent here: it is set only for a `PrunedScope` terminal, which this tree does not
// declare because nothing constructs one (see `ScopeTerminalsGapPrunedScope`). Its two uses upstream
// are both skips -- exclude the scope from `usedOutsideDeclaringScope`, and do not record its deps
// -- so a uniformly-false flag records every scope rather than wrongly dropping any.
type scopeBlockInfo struct {
	scope  ScopeId
	begins bool
}

// scopeBlockTraversal maps each block to the scope it begins or ends, upstream's
// `ScopeBlockTraversal.blockInfos`.
//
// This is the function that made three earlier stages necessary. It finds scopes by looking for a
// `Scope` terminal in the graph rather than by consulting a table, so before
// `BuildReactiveScopeTerminals` landed it returned an empty map on every function and every
// consumer of it silently produced nothing.
func scopeBlockTraversal(function *Function) map[static_single_assignment.BlockId]scopeBlockInfo {
	infos := map[static_single_assignment.BlockId]scopeBlockInfo{}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		if scope, ok := block.Terminal.(*Scope); ok {
			infos[scope.Block] = scopeBlockInfo{scope: scope.Scope, begins: true}
			infos[scope.Fallthrough] = scopeBlockInfo{scope: scope.Scope}
		}
	}
	return infos
}

// declaration records where a value was declared and which scopes were open at the time.
//
// Upstream's `Decl`. The scope stack is what `visitDependency` consults to decide that a value
// produced inside one scope and read inside another must be recorded as a declaration of the first.
type declaration struct {
	order      static_single_assignment.EvaluationOrder
	scopeStack []ScopeId
}

func containsIdentifier(list []static_single_assignment.IdentifierId, id static_single_assignment.IdentifierId) bool {
	for _, existing := range list {
		if existing == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// The pass
// ---------------------------------------------------------------------------

// CollectScopeDependencies computes every reactive scope's inputs, declarations and reassignments.
//
// Requires a graph whose reactive scopes have been built into `Scope` terminals: call
// `BuildReactiveScopeTerminals` first. A function with no such terminals gets an empty result, which
// is the correct answer for it and is also the characteristic failure of a caller that forgot --
// `TestDependenciesRequireScopeTerminals` pins the distinction.
//
// `identity` resolves a pre-merge scope id to the scope that actually survives and answers its final
// range, exactly as `BuildReactiveScopeTerminals` requires. Passing the pre-merge table instead
// produces plausible-looking output keyed on scopes that no longer exist.
func CollectScopeDependencies(function *Function, identity ScopeIdentity) *ScopeDependencies {
	result := &ScopeDependencies{}
	if function == nil || identity == nil {
		return result
	}

	terminals := scopeBlockTraversal(function)
	if len(terminals) == 0 {
		return result
	}

	usedOutside := findTemporariesUsedOutsideDeclaringScope(function, terminals)
	collected := collectTemporaries(function, usedOutside)

	// The optional chains. `PropagateScopeDependenciesHIR.ts:61` merges the traversal's temporaries
	// into the collector's map, so a value written inside a chain resolves to the full path with its
	// optionality recorded per step rather than to a bare root with a plain one.
	optional := CollectOptionalChainSidemap(function)
	if optional != nil {
		for id, dependency := range optional.TemporariesReadInOptional {
			collected[id] = dependency
		}
	}
	result.temporaries = collected
	collector := &dependencyCollector{
		function:                      function,
		temporaries:                   collected,
		declarations:                  map[static_single_assignment.DeclarationId]declaration{},
		reassignments:                 map[static_single_assignment.IdentifierId]declaration{},
		objectMethods:                 objectMethodValues(function),
		result:                        result,
		scopeRange:                    identity.RangeOf,
		processedOptionalInstructions: optionalProcessedInstructions(optional),
		processedOptionalTests:        optionalProcessedTests(optional),
	}

	// Parameters are declared before any instruction runs, so they are available to every scope.
	// Upstream declares them with `id: UNSET`, which is zero here and compares below every scope
	// start -- which is the intent: a parameter is always declared before the scope.
	for _, parameter := range function.Parameters {
		collector.declare(parameter.Identifier, declaration{})
	}

	collector.walk(function, terminals)
	collector.reduce(identity)
	return result
}

// CollectScopeDependenciesWithHoistable is the pass with the null analysis supplying path depth.
//
// `CollectScopeDependencies` truncates every dependency to its bare root, because `addDependency`
// stops at the first non-optional entry it cannot prove safe to hoist -- upstream's own behaviour
// under an empty hoistable set. This spelling runs `CollectHoistablePropertyLoads` first and hands
// its answer in, which is what lets a dependency name `props.a.b` rather than `props`.
//
// Both spellings are kept, and the difference between them is the measurement that shows this
// analysis works at all: the path-length distribution moves from every path at length zero to a real
// spread. See `TestHoistableAnalysisDeepensDependencyPaths`.
//
// The `ranges` argument must be the same table the scopes were assigned from. `Conflicts` on the
// result reports hoistable entries that disagreed about an access type, which upstream raises on.
func CollectScopeDependenciesWithHoistable(function *Function, scopes *ReactiveScopes,
	identity ScopeIdentity, ranges *mutation_aliasing.MutableRanges) *ScopeDependencies {
	result := &ScopeDependencies{}
	if function == nil || scopes == nil || identity == nil || ranges == nil {
		return result
	}

	terminals := scopeBlockTraversal(function)
	if len(terminals) == 0 {
		return result
	}

	usedOutside := findTemporariesUsedOutsideDeclaringScope(function, terminals)
	collected := collectTemporaries(function, usedOutside)

	// The optional chains. `PropagateScopeDependenciesHIR.ts:61` merges the traversal's temporaries
	// into the collector's map, so a value written inside a chain resolves to the full path with its
	// optionality recorded per step rather than to a bare root with a plain one.
	optional := CollectOptionalChainSidemap(function)
	if optional != nil {
		for id, dependency := range optional.TemporariesReadInOptional {
			// Later wins, matching `new Map([...temporaries, ...temporariesReadInOptional])`.
			// The optional traversal is the authoritative reconstruction for values it consumed.
			collected[id] = dependency
		}
	}
	result.temporaries = collected
	collector := &dependencyCollector{
		function:      function,
		temporaries:   collected,
		declarations:  map[static_single_assignment.DeclarationId]declaration{},
		reassignments: map[static_single_assignment.IdentifierId]declaration{},
		objectMethods: objectMethodValues(function),
		result:        result,
		scopeRange:    identity.RangeOf,
		hoistable: CollectHoistablePropertyLoads(
			function,
			scopes,
			identity,
			ranges,
			optionalHoistable(optional),
		),
		processedOptionalInstructions: optionalProcessedInstructions(optional),
		processedOptionalTests:        optionalProcessedTests(optional),
	}
	for _, parameter := range function.Parameters {
		collector.declare(parameter.Identifier, declaration{})
	}
	collector.walk(function, terminals)
	collector.reduce(identity)
	return result
}

// objectMethodValues returns the values produced by an ObjectMethod instruction.
//
// This is the recoverable half of `DependencyGapTypeExclusions`. Upstream rejects an object method
// as a dependency by consulting `Type::ObjectMethod`; types are nil in this IR, but the instruction
// that produces one is identifiable, and its lvalue is the value upstream's type would have named.
// Seven such instructions exist across the corpus, so this is a small but exact recovery rather than
// an approximation of the type check.
func objectMethodValues(function *Function) map[static_single_assignment.IdentifierId]bool {
	methods := map[static_single_assignment.IdentifierId]bool{}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			if _, ok := instruction.Value.(*ObjectMethod); ok {
				methods[instruction.LValue.Identifier] = true
			}
		}
	}
	return methods
}

// nestedAccesses returns every access a nested function makes, rooted at its own identifiers.
func nestedAccesses(function *Function, temps temporaries) []ReactiveScopeDependency {
	accesses, _ := nestedAccessesByBlock(function, temps)
	return accesses
}

// nestedAccessesByBlock is `nestedAccesses` plus the block each access was read in.
//
// The block matters because a deep path read inside a branch is a dependency -- the value is read
// there -- but is not evidence the object is safe to load early. Upstream keys its non-null sets by
// block for exactly this reason, and a scope reads the set at its own block, so a fact established
// inside a branch is available in that branch and nowhere else.
func nestedAccessesByBlock(function *Function, temps temporaries) ([]ReactiveScopeDependency,
	[]static_single_assignment.BlockId) {
	var accesses []ReactiveScopeDependency
	var blocks []static_single_assignment.BlockId
	processedOptionalTests := map[static_single_assignment.BlockId]bool{}
	processedInstructions := map[InstructionId]bool{}
	if optional := CollectOptionalChainSidemap(function); optional != nil {
		processedOptionalTests = optional.ProcessedOptionalTests
		processedInstructions = nestedOptionalInstructionsToDefer(function, optional)
	}
	record := func(block static_single_assignment.BlockId, dependency ReactiveScopeDependency) {
		accesses = append(accesses, dependency)
		blocks = append(blocks, block)
	}
	deferred := func(instruction *Instruction) bool {
		_, ok := temps[instruction.LValue.Identifier]
		return ok
	}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil || processedInstructions[instruction.Id] || deferred(instruction) {
				continue
			}
			switch value := instruction.Value.(type) {
			case *FunctionExpression:
				// A closure inside a closure. The pairing composes, so translate through this
				// function's own context and let the caller translate again.
				if int(value.Function) >= len(function.Functions) {
					continue
				}
				inner := function.Functions[value.Function]
				if inner == nil || len(value.Captures) != len(inner.Context) {
					continue
				}
				translate := map[static_single_assignment.IdentifierId]Place{}
				for index := range value.Captures {
					translate[inner.Context[index].Identifier] = value.Captures[index]
				}
				innerTemporaries := temporaries{}
				collectTemporariesInto(inner, map[static_single_assignment.DeclarationId]bool{}, innerTemporaries)
				if innerOptional := CollectOptionalChainSidemap(inner); innerOptional != nil {
					for identifier, dependency := range innerOptional.TemporariesReadInOptional {
						innerTemporaries[identifier] = dependency
					}
				}
				for _, dep := range nestedAccesses(inner, innerTemporaries) {
					outer, ok := translate[dep.Identifier]
					if !ok {
						continue
					}
					record(block.Id, temps.resolveWithPath(outer, dep.Path))
				}
			case *PropertyLoad:
				record(block.Id,
					temps.getProperty(value.Object, value.Property, value.Optional))
			default:
				EachPlace(instruction.Value, func(place Place, role PlaceRole) {
					if role != PlaceRoleDefine {
						record(block.Id, temps.resolve(place))
					}
				})
			}
		}
		if !processedOptionalTests[block.Id] {
			EachTerminalPlace(block.Terminal, func(place Place, role PlaceRole) {
				if role != PlaceRoleDefine {
					record(block.Id, temps.resolve(place))
				}
			})
		}
	}
	return accesses, blocks
}

// nestedOptionalInstructionsToDefer returns the optional-chain stores whose operand would only
// contribute a redundant prefix while translating a closure's captures.
//
// A consecutive optional link such as the first `?.` in `x?.y?.z` is consumed by the optional
// traversal: recording its StoreLocal operand as an access would add `x?.y`, which then prunes the
// real `x?.y?.z` dependency. A non-optional continuation is deliberately different. Upstream's
// current inference keeps the prefix in `propB?.x.y` and in the known-bug `x.a.b?.c.d?.e` case;
// those prefixes are what make preserved-memoization validation reject the less-specific inferred
// dependency. Only an immediate optional extension is therefore deferred here.
func nestedOptionalInstructionsToDefer(function *Function,
	optional *OptionalChainSidemap) map[InstructionId]bool {
	result := map[InstructionId]bool{}
	if function == nil || optional == nil {
		return result
	}
	for instructionId := range optional.ProcessedInstructions {
		instruction := function.Instructions[instructionId]
		if instruction == nil {
			continue
		}
		store, ok := instruction.Value.(*StoreLocal)
		if !ok {
			continue
		}
		prefix, ok := optional.TemporariesReadInOptional[store.LValue.Identifier]
		if !ok {
			continue
		}
		for _, candidate := range optional.TemporariesReadInOptional {
			if candidate.Identifier != prefix.Identifier || len(candidate.Path) != len(prefix.Path)+1 ||
				!candidate.Path[len(prefix.Path)].Optional ||
				!equalPaths(candidate.Path[:len(prefix.Path)], prefix.Path) {
				continue
			}
			result[instructionId] = true
			break
		}
	}
	return result
}

// blocksAlwaysReached returns the blocks of a function that run on every path through it.
//
// Post-dominance is the exact relation: a block runs on every path from entry to exit precisely when
// every such path passes through it.
func blocksAlwaysReached(function *Function) map[static_single_assignment.BlockId]bool {
	always := map[static_single_assignment.BlockId]bool{}
	if function == nil || len(function.Blocks) == 0 || function.Blocks[0] == nil {
		return always
	}
	entry := function.Blocks[0].Id
	always[entry] = true
	tree := computePostDominance(function)
	if tree == nil {
		return always
	}
	for current := entry; ; {
		next, found := tree.immediate[current]
		if !found || next == current {
			return always
		}
		always[next] = true
		current = next
	}
}

// appendWithoutOptionalDuplicates adds paths to a hoistable seed, dropping any whose prefixes
// disagree with one already committed about optionality.
//
// `ReactiveScopeDependencyTreeHIR`'s constructor states the precondition this maintains: "we expect
// these to not contain duplicates (e.g. both `a?.b` and `a.b`) only because
// `CollectHoistablePropertyLoads` merges duplicates when traversing the CFG". Upstream gets the
// invariant for free because every path it hands the tree came through `reduceMaybeOptionalChains`
// first.
//
// Paths recovered from inside a nested function do not. They are collected by walking the closure
// body directly, so `y.a?.b` from a callback can meet `y.a.b` from the enclosing function in one
// seed and the tree then reports a conflicting access type on an access the two agree about.
// Measured: with the nested seed disabled the corpus reports zero conflicts, with it enabled and
// unguarded eleven. Every conflict is this seam rather than the collector.
//
// The comparison is per prefix rather than per whole path, which is what the tree keys on. Two
// paths can share a prefix and diverge after it -- `[a][b?]` against `[a][b][c?]` -- and the tree
// walks entry by entry, so it sees `b` twice with different nullability while a whole-path key sees
// two unrelated strings. Measured: keying on the whole path took conflicts 11 to 5, and every
// survivor was a prefix disagreement of exactly that shape.
//
// The existing path wins. `reduceOptionalChains` has already run over the CFG-derived set and
// flipped every guard it could prove redundant, so its answer for a shared prefix is at least as
// resolved as anything the closure walk produces.
func appendWithoutOptionalDuplicates(seed []ReactiveScopeDependency,
	extra []ReactiveScopeDependency) []ReactiveScopeDependency {
	if len(extra) == 0 {
		return seed
	}
	// Every prefix already committed, and whether its last step reads optionally. Grown as paths
	// are admitted rather than built once, so two paths within `extra` are checked against each
	// other as well as against the seed.
	committed := map[string]bool{}
	for _, path := range seed {
		recordPrefixOptionality(path, committed)
	}

	combined := append([]ReactiveScopeDependency{}, seed...)
	for _, path := range extra {
		if prefixesDisagree(path, committed) {
			continue
		}
		recordPrefixOptionality(path, committed)
		combined = append(combined, path)
	}
	return combined
}

// recordPrefixOptionality notes, for every prefix of a path, whether its last step was optional.
//
// First writer wins per prefix, matching `hoistableTreeFor`, which keeps the node it already built
// and only counts a disagreement.
//
// Mutates: into each prefix not yet recorded is written, so one map commits the optionality of every path
func recordPrefixOptionality(path ReactiveScopeDependency, into map[string]bool) {
	var builder strings.Builder
	builder.WriteString(strconv.Itoa(int(path.Identifier)))
	for _, entry := range path.Path {
		builder.WriteByte('.')
		builder.WriteString(entry.Property)
		key := builder.String()
		if _, seen := into[key]; !seen {
			into[key] = entry.Optional
		}
	}
}

// prefixesDisagree reports whether any prefix of a path reads its step with the opposite
// optionality to what is already committed for that same prefix.
func prefixesDisagree(path ReactiveScopeDependency, committed map[string]bool) bool {
	var builder strings.Builder
	builder.WriteString(strconv.Itoa(int(path.Identifier)))
	for _, entry := range path.Path {
		builder.WriteByte('.')
		builder.WriteString(entry.Property)
		if optional, seen := committed[builder.String()]; seen && optional != entry.Optional {
			return true
		}
	}
	return false
}

// optionalHoistable is the sidemap's per-block hoistable set, or nil when there was no traversal.
func optionalHoistable(sidemap *OptionalChainSidemap) map[static_single_assignment.BlockId]ReactiveScopeDependency {
	if sidemap == nil {
		return nil
	}
	return sidemap.HoistableObjects
}

// optionalProcessedInstructions names the instructions whose operands were consumed while
// reconstructing an optional chain, or nil when no chains were found.
func optionalProcessedInstructions(sidemap *OptionalChainSidemap) map[InstructionId]bool {
	if sidemap == nil {
		return nil
	}
	return sidemap.ProcessedInstructions
}

// optionalProcessedTests names the blocks whose branch terminal an optional chain was recovered
// from, or nil when no chains were found.
func optionalProcessedTests(sidemap *OptionalChainSidemap) map[static_single_assignment.BlockId]bool {
	if sidemap == nil {
		return nil
	}
	return sidemap.ProcessedOptionalTests
}
