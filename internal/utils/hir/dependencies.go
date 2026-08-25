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
// needs its INPUTS: the values it reads that were produced before it began. Those inputs are
// dependencies, and a dependency is not an identifier -- it is an ACCESS PATH rooted at one.
//
//	ReactiveScopeDependency { Identifier, Reactive, Path []DependencyPathEntry }
//	DependencyPathEntry     { Property string, Optional bool }
//
// So `props.a.b` is one dependency: root `props`, path `[a, b]`. This matters because a scope
// reading only `props.a.b` should not invalidate when `props.c` changes, and because the dedup
// predicate compares the whole path. Upstream's `areEqualPaths` (bundle line 47352) compares each
// entry's property AND its `Optional` flag, so `a?.b` and `a.b` are DIFFERENT dependencies. That
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
// # Its input is the GRAPH, not the scope table, which is why three stages had to land first
//
// The single most surprising thing about this pass is that it never reads a scope side table to
// find its subjects. It recovers scopes by walking blocks for a `Scope` TERMINAL, through
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
// OUTPUT field belongs to this file and is silent on whether the file's INPUTS exist. Both levels
// have to be checked, and the second level is "is the type ever CONSTRUCTED", not merely declared.
// `Optional` is the standing example: it is a declared terminal that nothing in this tree builds.
//
// # DIVERGENCE FROM React: oxc sorts a collection React does not
//
// `ReactiveScopeDependencyTreeHIR::new` in oxc sorts its hoistable objects so optional-first entries
// precede non-optional ones, with a comment claiming this "matches the TS behavior". React does no
// such sort (bundle 46967): it iterates `hoistableObjects` in insertion order and raises a hard
// `CompilerError.invariant('Conflicting access types')` when two entries disagree, where oxc
// silently takes first-wins via `or_insert_with`.
//
// React is the truth, so no sort is performed here. The divergence is recorded rather than split
// because it is unobservable in this tree today for a reason that is measured rather than assumed:
// the hoistable set is empty here (see `DependencyGapOptionalChains`), so there is nothing to sort
// and nothing to conflict. It becomes real the moment optional chains are reconstructed.
//
// # Termination is a single ordered walk, with no fixpoint anywhere
//
// There is no worklist and no iteration to convergence. `CollectScopeDependencies` makes ONE pass
// over the blocks in order, maintaining a stack of active scopes. Every block is visited once, every
// instruction once, and every operand once. The dependency tree is built by insertion and read once
// by `deriveMinimalDependencies`, whose recursion is over a tree whose depth is the longest property
// path and whose nodes are finite and fixed before the walk begins.
//
// This is a deliberately weaker claim than the passes upstream of it needed, and it is the whole
// reason `propagate_non_null` is NOT in this file. That analysis is a bounded loop of at most 100
// alternating forward and backward passes over the CFG with a `changed` flag -- a bound, not a
// proof, and if real code ever reached 100 the answer would be silently truncated. It is a separate
// pass with a separate termination story and it is not required for what this file computes; see
// `DependencyGapNullPropagation`. `TestDependencyCollectionIsASinglePass` pins the walk empirically.
//
// # The distribution that proves these dependencies are real
//
// A count cannot separate a working pass from one that collects nothing, so the headline is a
// CROSS-TABULATION of dependencies per scope against scope member count, over 400 corpus files and
// 677 outermost functions. The control cell is scopes carrying ZERO dependencies: a pass that
// collected nothing would put every scope there, and a pass that over-collected would empty it.
//
// See `TestDependencyDistributionIsReal`, which pins the shape rather than the corpus numbers.
package hir

// DependencyPathEntry is one step in an access path: the `.b` in `props.a.b`.
//
// `Optional` records that this step came from `?.` rather than `.`. It is part of the entry's
// IDENTITY rather than a decoration: upstream's `areEqualPaths` compares it, so `a?.b` and `a.b` are
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
	Identifier IdentifierId
	Reactive   bool
	Path       []DependencyPathEntry
}

// equalPaths reports whether two access paths are identical, upstream's `areEqualPaths`.
//
// Compares `Optional` as well as `Property`, which is what makes `a?.b` and `a.b` distinct. A plain
// property-name comparison passes every fixture built from unguarded accesses and silently merges
// the guarded ones, so the flag is compared here rather than at the call site.
func equalPaths(a, b []DependencyPathEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index].Property != b[index].Property || a[index].Optional != b[index].Optional {
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
	// DependencyGapOptionalChains is upstream's optional-chain reconstruction, not performed here.
	//
	// React's `collectOptionalChainSidemap` recovers "that was one property chain" from the SHAPE of
	// a lowered control-flow graph: `traverseFunctionOptional` fires only on an `Optional` terminal,
	// and `matchOptionalTestBlock` then pattern-matches a `Branch` whose consequent block holds
	// exactly two instructions (a `PropertyLoad` and a matching `StoreLocal`) ending in a
	// `Goto{Break}`, against an alternate of exactly two (`Primitive`, `StoreLocal`).
	//
	// This lowering never produces that shape. Measured on `props?.a?.b` plus `props.fn?.()`: 0
	// `Optional` terminals, 0 `Branch` terminals, 2 `PropertyLoad` with `Optional` set. Corpus-wide,
	// 0 `Optional` terminals against 166 optional `PropertyLoad`s out of 4,766. Optionality lives on
	// the instruction here, not as branching, so this is not a port -- it is a different algorithm,
	// and choosing it is a design decision rather than a transcription.
	//
	// The consequence for THIS pass is bounded and was measured rather than assumed.
	// `collectOptionalChainSidemap` initialises all three of its outputs empty and only ever ADDS on
	// encountering an `Optional` terminal, so with none it returns three empty collections -- which
	// is a state every consumption site here already tolerates. The effect is that the 166 optional
	// loads are skipped, not mis-collected. That is an honest subset rather than a silent zero.
	DependencyGapOptionalChains DependencyGap = iota

	// DependencyGapNullPropagation is upstream's hoistable-property analysis, not performed here.
	//
	// `collectNonNullsInBlocks` and `propagateNonNull` are a bidirectional dataflow analysis that
	// decides which property loads may be HOISTED out of a scope: reading `props.a.b` before the
	// scope runs is safe only if `props.a` is known non-null there, because hoisting an access past
	// a point where the value might be null moves or invents a crash.
	//
	// Omitting it costs PRECISION, not correctness, and the direction of the error is the safe one.
	// The hoistable set is an input to `ReactiveScopeDependencyTreeHIR`, where it can only PROMOTE
	// an optional access to an unconditional one. With an empty set every access stays at the
	// conditional reading, so this pass reports dependencies at least as deep and never claims an
	// access is safe to hoist when it is not.
	//
	// It is also a separate pass because its termination story is genuinely different: a bounded
	// loop of at most 100 alternating forward and backward passes with a `changed` flag, which is a
	// BOUND rather than a proof. Everything in this file is a single ordered walk.
	DependencyGapNullPropagation

	// DependencyGapTypeExclusions is upstream's two type-based dependency filters, not applied.
	//
	// `checkValidDependency` rejects two kinds of value outright: a ref's `.current` (via
	// `isRefValueType`) and an object method (`Type::ObjectMethod`). Both are EXCLUSIONS, so their
	// absence can only ADD dependencies, never drop one.
	//
	// `Identifier.Type` is declared `any` in this IR and left nil by lowering: measured over the
	// corpus, 0 of 110,265 identifiers carry a type. So neither predicate is expressible. The
	// exposure was measured rather than estimated: 7 `ObjectMethod` instructions and 3 `.current`
	// property loads across 4,766 loads in 677 functions. The object-method half is recoverable
	// structurally -- an `ObjectMethod` INSTRUCTION is identifiable without types -- and is applied
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
		DependencyGapNullPropagation,
		DependencyGapTypeExclusions,
	}
}

// ScopeDependencies is the table this pass produces: the three scope fields, keyed by scope.
//
// A side table for the reason `ReactiveScopes` is one: the scope lives in a table rather than on the
// identifier, so its outputs live beside it. The zero value is empty and ready to read.
type ScopeDependencies struct {
	dependencies  map[ScopeId][]ReactiveScopeDependency
	declarations  map[ScopeId][]IdentifierId
	reassignments map[ScopeId][]IdentifierId
	order         []ScopeId
}

// DependenciesOf returns a scope's inputs, or nil for a scope with none.
//
// The slice is the table's own and must not be modified by a caller.
func (d *ScopeDependencies) DependenciesOf(scope ScopeId) []ReactiveScopeDependency {
	if d == nil || d.dependencies == nil {
		return nil
	}
	return d.dependencies[scope]
}

// DeclarationsOf returns the values a scope produces that are used outside it.
//
// Upstream stores `(IdentifierId, ReactiveScopeDeclaration)` pairs where the second names the
// declaring scope. The scope is the map key here, so the pair would store it twice.
func (d *ScopeDependencies) DeclarationsOf(scope ScopeId) []IdentifierId {
	if d == nil || d.declarations == nil {
		return nil
	}
	return d.declarations[scope]
}

// ReassignmentsOf returns the bindings a scope reassigns.
func (d *ScopeDependencies) ReassignmentsOf(scope ScopeId) []IdentifierId {
	if d == nil || d.reassignments == nil {
		return nil
	}
	return d.reassignments[scope]
}

// Ids returns every scope this pass recorded, in the order it closed them.
//
// Returned as a slice rather than by ranging a map so a caller iterating scopes gets the same
// sequence every run. Go map iteration is randomised, and a pass whose output order varies makes a
// cache non-reproducible without ever producing a wrong answer.
func (d *ScopeDependencies) Ids() []ScopeId {
	if d == nil {
		return nil
	}
	return d.order
}

// Len reports how many scopes carry an entry, for measurement.
func (d *ScopeDependencies) Len() int {
	if d == nil {
		return 0
	}
	return len(d.order)
}

// ---------------------------------------------------------------------------
// The dependency tree, and the minimal set it derives
// ---------------------------------------------------------------------------

// propertyAccessType is how a node in the dependency tree was reached.
//
// Two independent bits packed into upstream's four-variant enum: whether the access was guarded by
// `?.` (optional) or not (unconditional), and whether the node is itself a DEPENDENCY or merely a
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
// as a dependency on either path, it is one. "Unconditional" is an INTERSECTION expressed by De
// Morgan -- the result is optional only when BOTH readings were optional, so a value read once
// guarded and once unguarded is unguarded, because the unguarded read already happened.
func mergeAccess(a, b propertyAccessType) propertyAccessType {
	unconditional := !(isOptionalAccess(a) && isOptionalAccess(b))
	dependency := isDependencyAccess(a) || isDependencyAccess(b)
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

// dependencyNode is one value in the access-path tree a scope's dependencies form.
//
// `properties` is ordered rather than a bare map because `deriveMinimalDependencies` walks it and
// the walk's order becomes the output's order. Upstream uses a JS `Map`, which is insertion-ordered;
// a Go map is randomised, so insertion order is kept explicitly in `keys`.
type dependencyNode struct {
	properties map[string]*dependencyNode
	keys       []string
	access     propertyAccessType
	reactive   bool
}

func newDependencyNode(access propertyAccessType, reactive bool) *dependencyNode {
	return &dependencyNode{properties: map[string]*dependencyNode{}, access: access, reactive: reactive}
}

// makeOrMergeProperty is upstream's `makeOrMergeProperty`: descend one step, creating or merging.
func (n *dependencyNode) makeOrMergeProperty(property string, access propertyAccessType) *dependencyNode {
	if existing, ok := n.properties[property]; ok {
		existing.access = mergeAccess(existing.access, access)
		return existing
	}
	child := newDependencyNode(access, n.reactive)
	n.properties[property] = child
	n.keys = append(n.keys, property)
	return child
}

// hoistableNode is one value in the tree of accesses proven safe to hoist out of a scope.
//
// Always empty in this tree: the analysis that populates it is `DependencyGapNullPropagation`. Kept
// as a real type rather than elided because its ABSENCE is what makes `addDependency` truncate, and
// a reader tracing why paths are short should find the mechanism rather than a missing parameter.
type hoistableNode struct {
	properties map[string]*hoistableNode
	// nonNull is upstream's `HoistableAccessType::NonNull`, meaning this access is known not to
	// throw and may therefore be read before the scope runs.
	nonNull bool
}

// dependencyTree accumulates a scope's accesses and reduces them to a minimal set.
//
// Upstream's `ReactiveScopeDependencyTreeHIR`. Two trees, not one: `hoistable` is what may be read
// early, `roots` is what was actually read, and `addDependency` walks them in lockstep.
type dependencyTree struct {
	hoistable map[IdentifierId]*hoistableNode
	roots     map[IdentifierId]*dependencyNode
	rootOrder []IdentifierId
}

// newDependencyTree builds the tree, seeding it with the accesses proven hoistable.
//
// # DIVERGENCE FROM oxc, recorded at the line that resolves it
//
// oxc sorts `hoistable_objects` here so that optional-first entries precede non-optional ones,
// commenting that this "matches the TS behavior". It does not. React iterates the collection in
// insertion order with no sort (bundle 46967) and raises `CompilerError.invariant('Conflicting
// access types')` when two entries disagree about a property, where oxc silently keeps the first.
//
// React is the truth, so there is no sort. The divergence is unobservable here for a measured
// reason rather than an assumed one: `hoistable` is always empty in this tree, so there is nothing
// to order and nothing to conflict. It becomes real if optional chains are ever reconstructed.
func newDependencyTree(hoistable map[IdentifierId]*hoistableNode) *dependencyTree {
	if hoistable == nil {
		hoistable = map[IdentifierId]*hoistableNode{}
	}
	return &dependencyTree{hoistable: hoistable, roots: map[IdentifierId]*dependencyNode{}}
}

// addDependency records one access path, truncating it where the path stops being safe to hoist.
//
// # Why every path truncates to its root in this tree, and why that is upstream's own answer
//
// The loop walks the access path and the hoistable tree together. An entry survives only if the
// hoistable cursor says the object is already known non-null, or the entry itself is optional; a
// non-optional entry under an unknown object hits `break` and the path stops there.
//
// With an empty hoistable set the cursor is nil at every step, so a path like `props.a.b` truncates
// to bare `props`. That is not an approximation of upstream -- it is exactly what React does when
// handed an empty hoistable set (bundle 47019, the `else { break; }` arm), and it is the SAFE
// direction: the scope is reported as depending on all of `props` rather than on `props.a.b`, so it
// invalidates more often than strictly necessary and never less. Recovering the deeper paths is
// what `DependencyGapNullPropagation` would buy.
func (t *dependencyTree) addDependency(dep ReactiveScopeDependency) {
	root, ok := t.roots[dep.Identifier]
	if !ok {
		root = newDependencyNode(unconditionalAccess, dep.Reactive)
		t.roots[dep.Identifier] = root
		t.rootOrder = append(t.rootOrder, dep.Identifier)
	}

	cursor := root
	var hoistableCursor *hoistableNode
	if seed, present := t.hoistable[dep.Identifier]; present {
		hoistableCursor = seed
	}

	for _, entry := range dep.Path {
		var nextHoistable *hoistableNode
		var access propertyAccessType
		switch {
		case hoistableCursor != nil && hoistableCursor.nonNull:
			nextHoistable = hoistableCursor.properties[entry.Property]
			access = unconditionalAccess
		case entry.Optional:
			if hoistableCursor != nil {
				nextHoistable = hoistableCursor.properties[entry.Property]
			}
			access = optionalAccess
		default:
			// The path stops being hoistable here. See this function's comment: with no null
			// analysis this is reached on the first non-optional entry, which is upstream's own
			// behaviour under an empty hoistable set rather than a shortcut taken here.
			cursor.access = mergeAccess(cursor.access, optionalDependency)
			return
		}
		cursor = cursor.makeOrMergeProperty(entry.Property, access)
		hoistableCursor = nextHoistable
	}

	cursor.access = mergeAccess(cursor.access, optionalDependency)
}

// deriveMinimalDependencies returns the shallowest set of paths covering every access.
//
// A node marked as a dependency is emitted and its children are NOT walked, because depending on
// `props.a` already covers `props.a.b`. That pruning is the whole point of the tree: a scope reading
// `props.a`, `props.a.b` and `props.a.c` reports one dependency rather than three.
func (t *dependencyTree) deriveMinimalDependencies() []ReactiveScopeDependency {
	var results []ReactiveScopeDependency
	for _, rootId := range t.rootOrder {
		root := t.roots[rootId]
		collectMinimalInSubtree(root, rootId, nil, &results)
	}
	return results
}

// collectMinimalInSubtree is upstream's `collectMinimalDependenciesInSubtree`.
//
// Recursion depth is the longest access path, and the tree is finite and fully built before this
// runs, so this terminates without any convergence argument. See the package comment on termination.
func collectMinimalInSubtree(node *dependencyNode, root IdentifierId,
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

// temporaries maps a value to the access path it holds, so `t = props.a; t.b` reads as `props.a.b`.
//
// Lowering breaks every expression into single instructions, so `props.a.b` becomes a `PropertyLoad`
// into a temporary and a second `PropertyLoad` off it. Without this map the second load's object is
// an anonymous value and the dependency would be recorded as that temporary rather than as a path
// through `props`. This is what reassembles them.
type temporaries map[IdentifierId]ReactiveScopeDependency

// getProperty resolves one property access into a dependency, upstream's `getProperty`.
//
// When the object is itself a known temporary the access EXTENDS that temporary's path; otherwise it
// starts a new path rooted at the object. The extension copies the path rather than appending in
// place, because two accesses off one temporary must not share a backing array.
func (t temporaries) getProperty(object Place, property string, optional bool) ReactiveScopeDependency {
	if resolved, ok := t[object.Identifier]; ok {
		path := make([]DependencyPathEntry, len(resolved.Path), len(resolved.Path)+1)
		copy(path, resolved.Path)
		path = append(path, DependencyPathEntry{Property: property, Optional: optional})
		return ReactiveScopeDependency{
			Identifier: resolved.Identifier,
			Reactive:   resolved.Reactive,
			Path:       path,
		}
	}
	return ReactiveScopeDependency{
		Identifier: object.Identifier,
		Reactive:   object.Reactive,
		Path:       []DependencyPathEntry{{Property: property, Optional: optional}},
	}
}

// resolve returns the dependency a place names, following the temporaries map.
func (t temporaries) resolve(place Place) ReactiveScopeDependency {
	if resolved, ok := t[place.Identifier]; ok {
		path := make([]DependencyPathEntry, len(resolved.Path))
		copy(path, resolved.Path)
		return ReactiveScopeDependency{
			Identifier: resolved.Identifier,
			Reactive:   resolved.Reactive,
			Path:       path,
		}
	}
	return ReactiveScopeDependency{Identifier: place.Identifier, Reactive: place.Reactive}
}

// collectTemporaries builds the sidemap, upstream's `collectTemporariesSidemap`.
//
// A value is recorded only when it is NOT used outside the scope that declares it: a temporary read
// from elsewhere is a real value with its own identity rather than a rename of the thing it loaded.
// `usedOutside` carries that answer, computed by `findTemporariesUsedOutsideDeclaringScope`.
//
// The `LoadLocal` arm requires the destination to be ANONYMOUS and the source to be NAMED, which is
// upstream's `lvalue.identifier.name === null && place.identifier.name !== null`. That asymmetry is
// what keeps this a map from temporaries to real values rather than a general alias table.
func collectTemporaries(function *Function, usedOutside map[DeclarationId]bool) temporaries {
	result := temporaries{}
	collectTemporariesInto(function, usedOutside, result)
	return result
}

// collectTemporariesInto walks one function, recursing into nested ones.
//
// # DIVERGENCE FROM React: nested functions get their own map here
//
// Upstream threads ONE `temporaries` map through the recursion into nested functions, keyed by
// `IdentifierId`. That is safe there because ids are unique across the whole compilation. They are
// per-function here -- `FindDisjointMutableValues` records why -- so a shared map would conflate
// values: measured on a two-closure function, 14 collisions across 30 distinct ids.
//
// Nested functions are therefore walked with a FRESH map, whose results are discarded. That is
// narrower than upstream, and the narrowing is in the safe direction: a dependency inside a nested
// function resolves to the nested value rather than to a path through the outer one, so a path is
// reported shallower rather than wrong. Recorded because a reader diffing against the bundle will
// see the shared map missing.
func collectTemporariesInto(function *Function, usedOutside map[DeclarationId]bool, into temporaries) {
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
					into.getProperty(value.Object, value.Property, false)
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
	terminals map[BlockId]scopeBlockInfo) map[DeclarationId]bool {
	declaringScope := map[DeclarationId]ScopeId{}
	usedOutside := map[DeclarationId]bool{}
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
	// after lowering, so slice order IS execution order and `block.Id` is the addressing key. Using
	// the slice INDEX as a BlockId would be a silent correctness bug, because the two move
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
// are both SKIPS -- exclude the scope from `usedOutsideDeclaringScope`, and do not record its deps
// -- so a uniformly-false flag records every scope rather than wrongly dropping any.
type scopeBlockInfo struct {
	scope  ScopeId
	begins bool
}

// scopeBlockTraversal maps each block to the scope it begins or ends, upstream's
// `ScopeBlockTraversal.blockInfos`.
//
// This is the function that made three earlier stages necessary. It finds scopes by looking for a
// `Scope` TERMINAL in the graph rather than by consulting a table, so before
// `BuildReactiveScopeTerminals` landed it returned an empty map on every function and every
// consumer of it silently produced nothing.
func scopeBlockTraversal(function *Function) map[BlockId]scopeBlockInfo {
	infos := map[BlockId]scopeBlockInfo{}
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
// produced inside one scope and read inside another must be recorded as a DECLARATION of the first.
type declaration struct {
	order      EvaluationOrder
	scopeStack []ScopeId
}

// dependencyCollector is the walk's state, upstream's `DependencyCollectionContext`.
type dependencyCollector struct {
	function      *Function
	temporaries   temporaries
	scopeStack    []ScopeId
	dependencies  [][]ReactiveScopeDependency
	declarations  map[DeclarationId]declaration
	reassignments map[IdentifierId]declaration
	objectMethods map[IdentifierId]bool
	result        *ScopeDependencies
	// scopeRange answers a scope's final range, which `checkValidDependency` compares against.
	scopeRange func(ScopeId) MutableRange
}

func (c *dependencyCollector) currentScope() (ScopeId, bool) {
	if len(c.scopeStack) == 0 {
		return 0, false
	}
	return c.scopeStack[len(c.scopeStack)-1], true
}

func (c *dependencyCollector) enterScope(scope ScopeId) {
	c.scopeStack = append(c.scopeStack, scope)
	c.dependencies = append(c.dependencies, nil)
}

// exitScope closes a scope, recording its dependencies and propagating them outward.
//
// The propagation is what makes a nested scope's inputs also inputs of its parent: a value the inner
// scope needs must be available before the outer one runs. Only dependencies that are still VALID
// at the outer level propagate, which `checkValidDependency` decides.
func (c *dependencyCollector) exitScope(scope ScopeId) {
	if len(c.scopeStack) == 0 {
		return
	}
	scoped := c.dependencies[len(c.dependencies)-1]
	c.dependencies = c.dependencies[:len(c.dependencies)-1]
	c.scopeStack = c.scopeStack[:len(c.scopeStack)-1]

	for _, dep := range scoped {
		if len(c.dependencies) > 0 && c.checkValidDependency(dep) {
			c.dependencies[len(c.dependencies)-1] =
				append(c.dependencies[len(c.dependencies)-1], dep)
		}
	}

	if c.result.dependencies == nil {
		c.result.dependencies = map[ScopeId][]ReactiveScopeDependency{}
	}
	if _, seen := c.result.dependencies[scope]; !seen {
		c.result.order = append(c.result.order, scope)
	}
	c.result.dependencies[scope] = append(c.result.dependencies[scope], scoped...)
}

func (c *dependencyCollector) declare(id IdentifierId, decl declaration) {
	identifier := c.function.Identifiers[id]
	if identifier == nil {
		return
	}
	if _, exists := c.declarations[identifier.Declaration]; !exists {
		c.declarations[identifier.Declaration] = decl
	}
}

// checkValidDependency decides whether a value read inside a scope is an INPUT to it.
//
// A value is a dependency only when it was declared BEFORE the scope began. A value produced inside
// the scope is not an input to it -- it is part of what the scope computes. That comparison is the
// whole predicate, and it is why the scope's final range matters: comparing against a pre-alignment
// range would misclassify values declared in the widened region.
//
// The two type-based exclusions upstream also applies are `DependencyGapTypeExclusions`. The object
// method half is recovered structurally here rather than through a type, because an `ObjectMethod`
// instruction is identifiable without one; the ref half is not expressible and is 3 accesses.
func (c *dependencyCollector) checkValidDependency(dep ReactiveScopeDependency) bool {
	if c.objectMethods[dep.Identifier] {
		return false
	}
	scope, ok := c.currentScope()
	if !ok {
		return false
	}
	identifier := c.function.Identifiers[dep.Identifier]
	if identifier == nil {
		return false
	}
	decl, found := c.reassignments[dep.Identifier]
	if !found {
		decl, found = c.declarations[identifier.Declaration]
	}
	if !found {
		return false
	}
	return decl.order < c.scopeRange(scope).Start
}

// visitDependency records one access, and records a DECLARATION when it crosses a scope boundary.
//
// The second half is the `declarations` field: a value produced inside a scope and read outside it
// must be exported from that scope, because the memoized block has to return it.
func (c *dependencyCollector) visitDependency(dep ReactiveScopeDependency) {
	identifier := c.function.Identifiers[dep.Identifier]
	if identifier == nil {
		return
	}
	if original, ok := c.declarations[identifier.Declaration]; ok && len(original.scopeStack) > 0 {
		for _, declaringScope := range original.scopeStack {
			if c.scopeIsActive(declaringScope) {
				continue
			}
			if c.result.declarations == nil {
				c.result.declarations = map[ScopeId][]IdentifierId{}
			}
			if !containsIdentifier(c.result.declarations[declaringScope], dep.Identifier) {
				c.result.declarations[declaringScope] =
					append(c.result.declarations[declaringScope], dep.Identifier)
			}
		}
	}
	if len(c.dependencies) == 0 {
		return
	}
	if c.checkValidDependency(dep) {
		c.dependencies[len(c.dependencies)-1] =
			append(c.dependencies[len(c.dependencies)-1], dep)
	}
}

// visitReassignment records that the current scope writes to a binding declared outside it.
func (c *dependencyCollector) visitReassignment(place Place) {
	scope, ok := c.currentScope()
	if !ok {
		return
	}
	if !c.checkValidDependency(ReactiveScopeDependency{
		Identifier: place.Identifier, Reactive: place.Reactive}) {
		return
	}
	if c.result.reassignments == nil {
		c.result.reassignments = map[ScopeId][]IdentifierId{}
	}
	target := c.function.Identifiers[place.Identifier]
	if target == nil {
		return
	}
	for _, existing := range c.result.reassignments[scope] {
		if other := c.function.Identifiers[existing]; other != nil &&
			other.Declaration == target.Declaration {
			return
		}
	}
	c.result.reassignments[scope] = append(c.result.reassignments[scope], place.Identifier)
}

func (c *dependencyCollector) scopeIsActive(scope ScopeId) bool {
	for _, candidate := range c.scopeStack {
		if candidate == scope {
			return true
		}
	}
	return false
}

func containsIdentifier(list []IdentifierId, id IdentifierId) bool {
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
// Requires a graph whose reactive scopes have been built into `Scope` TERMINALS: call
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
	collector := &dependencyCollector{
		function:      function,
		temporaries:   collectTemporaries(function, usedOutside),
		declarations:  map[DeclarationId]declaration{},
		reassignments: map[IdentifierId]declaration{},
		objectMethods: objectMethodValues(function),
		result:        result,
		scopeRange:    identity.RangeOf,
	}

	// Parameters are declared before any instruction runs, so they are available to every scope.
	// Upstream declares them with `id: UNSET`, which is zero here and compares below every scope
	// start -- which is the intent: a parameter is always declared before the scope.
	for _, param := range function.Params {
		collector.declare(param.Identifier, declaration{})
	}

	collector.walk(function, terminals)
	collector.reduce(identity)
	return result
}

// objectMethodValues returns the values produced by an ObjectMethod instruction.
//
// This is the recoverable half of `DependencyGapTypeExclusions`. Upstream rejects an object method
// as a dependency by consulting `Type::ObjectMethod`; types are nil in this IR, but the INSTRUCTION
// that produces one is identifiable, and its lvalue is the value upstream's type would have named.
// Seven such instructions exist across the corpus, so this is a small but exact recovery rather than
// an approximation of the type check.
func objectMethodValues(function *Function) map[IdentifierId]bool {
	methods := map[IdentifierId]bool{}
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

// walk is the single ordered pass over the graph, upstream's `handleFunctionDeps`.
//
// One traversal of the blocks in execution order, maintaining the scope stack. Every block, every
// instruction and every operand is visited exactly once, which is the whole termination argument:
// the work is linear in the graph and the loops are over slices fixed before the walk begins.
func (c *dependencyCollector) walk(function *Function, terminals map[BlockId]scopeBlockInfo) {
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		if info, ok := terminals[block.Id]; ok {
			if info.begins {
				c.enterScope(info.scope)
			} else {
				c.exitScope(info.scope)
			}
		}

		// Phi operands are visited before the instructions, because a phi is conceptually evaluated
		// on entry to the block. `Phi.Operands` is a Go map, so the ordered accessor is required:
		// ranging it directly would make the output order vary between runs.
		for _, phi := range block.Phis {
			// `PhiOperandsInOrder` returns the PREDECESSOR block ids in sorted order, because
			// `Phi.Operands` is a Go map and ranging it directly would vary between runs.
			//
			// # A mutation replacing this with a bare map range SURVIVES, and the reason is not
			// that a fixture is missing
			//
			// The first reading assumed the sweep had found a test gap and a determinism fixture
			// repeated forty times was written for it. It still survived, which is the signal that
			// the HYPOTHESIS was wrong rather than the fixture weak, so the fixture was removed
			// rather than strengthened.
			//
			// The real reason, measured: every phi in this tree sits OUTSIDE every reactive scope.
			// On a fixture with a multi-operand phi carrying two distinct operand values, phis
			// inside a scope = 0 and phis outside every scope = 1. `visitDependency` returns early
			// when the dependency stack is empty, so no phi operand currently reaches the output at
			// all and their order cannot be observed.
			//
			// The ordered accessor is kept anyway, for the reason `Sets` sorts in `scopes.go`: the
			// cost is one sort and the alternative is a latent nondeterminism that appears the day
			// the scopes widen. This verdict EXPIRES the moment a scope contains a phi -- which
			// `AlignReactiveScopes` widening a scope across a control-flow join would produce.
			for _, predecessor := range PhiOperandsInOrder(phi) {
				c.visitDependency(c.temporaries.resolve(phi.Operands[predecessor]))
			}
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			c.handleInstruction(instruction)
		}

		EachTerminalPlace(block.Terminal, func(place Place, role PlaceRole) {
			if role != PlaceRoleDefine {
				c.visitDependency(c.temporaries.resolve(place))
			}
		})
	}

	// Anything still open at the end is closed in reverse, so a scope whose fallthrough was never
	// reached still records its dependencies rather than discarding them.
	for len(c.scopeStack) > 0 {
		c.exitScope(c.scopeStack[len(c.scopeStack)-1])
	}
}

// handleInstruction visits one instruction, upstream's `handleInstruction`.
//
// The per-kind arms are upstream's and the order within each matters: a store visits its VALUE
// before declaring its lvalue, so `x = x + 1` reads the old `x` as a dependency rather than seeing
// its own definition.
func (c *dependencyCollector) handleInstruction(instruction *Instruction) {
	decl := declaration{order: instruction.Order, scopeStack: c.copyScopeStack()}
	c.declare(instruction.LValue.Identifier, decl)

	switch value := instruction.Value.(type) {
	case *PropertyLoad:
		c.visitDependency(c.temporaries.getProperty(value.Object, value.Property, value.Optional))
	case *StoreLocal:
		c.visitDependency(c.temporaries.resolve(value.Value))
		if value.Kind == InstructionKindReassign {
			c.visitReassignment(value.LValue)
			c.reassignments[value.LValue.Identifier] = decl
		}
		c.declare(value.LValue.Identifier, decl)
	case *StoreContext:
		c.visitDependency(c.temporaries.resolve(value.Value))
		if value.Kind == InstructionKindReassign {
			c.visitReassignment(value.LValue)
			c.reassignments[value.LValue.Identifier] = decl
		}
		c.declare(value.LValue.Identifier, decl)
	case *DeclareLocal:
		c.declare(value.LValue.Identifier, decl)
	case *DeclareContext:
		c.declare(value.LValue.Identifier, decl)
	case *Destructure:
		c.visitDependency(c.temporaries.resolve(value.Value))
		// The shared `eachPatternPlace` yields defaults and computed keys as USES alongside the
		// bindings, which is richer than upstream's `eachPatternOperand` and exactly right here: a
		// default value is read by the destructure and is a genuine dependency.
		eachPatternPlace(value.LValue, func(place Place, role PlaceRole) {
			if role != PlaceRoleDefine {
				c.visitDependency(c.temporaries.resolve(place))
				return
			}
			if value.Kind == InstructionKindReassign {
				c.visitReassignment(place)
				c.reassignments[place.Identifier] = decl
			}
			c.declare(place.Identifier, decl)
		})
	default:
		EachPlace(instruction.Value, func(place Place, role PlaceRole) {
			if role != PlaceRoleDefine {
				c.visitDependency(c.temporaries.resolve(place))
			}
		})
	}
}

func (c *dependencyCollector) copyScopeStack() []ScopeId {
	if len(c.scopeStack) == 0 {
		return nil
	}
	copied := make([]ScopeId, len(c.scopeStack))
	copy(copied, c.scopeStack)
	return copied
}

// reduce turns each scope's raw accesses into the minimal covering set of dependencies.
//
// This is the second half of upstream's entry point: build a tree per scope, add every access, take
// the minimal set, and append each candidate that is not already present. The dedup predicate is
// upstream's -- same DECLARATION and equal paths -- so two values of one reassigned binding
// deduplicate against each other, which comparing identifiers would not do.
func (c *dependencyCollector) reduce(identity ScopeIdentity) {
	if c.result.dependencies == nil {
		return
	}
	for _, scope := range c.result.order {
		accesses := c.result.dependencies[scope]
		if len(accesses) == 0 {
			continue
		}
		tree := newDependencyTree(nil)
		for _, access := range accesses {
			tree.addDependency(access)
		}
		var reduced []ReactiveScopeDependency
		for _, candidate := range tree.deriveMinimalDependencies() {
			if !c.alreadyPresent(reduced, candidate) {
				reduced = append(reduced, candidate)
			}
		}
		c.result.dependencies[scope] = reduced
	}
}

// alreadyPresent is upstream's dedup predicate: same declaration and equal paths.
func (c *dependencyCollector) alreadyPresent(existing []ReactiveScopeDependency,
	candidate ReactiveScopeDependency) bool {
	candidateIdentifier := c.function.Identifiers[candidate.Identifier]
	if candidateIdentifier == nil {
		return false
	}
	for _, present := range existing {
		presentIdentifier := c.function.Identifiers[present.Identifier]
		if presentIdentifier == nil {
			continue
		}
		if presentIdentifier.Declaration == candidateIdentifier.Declaration &&
			equalPaths(present.Path, candidate.Path) {
			return true
		}
	}
	return false
}
