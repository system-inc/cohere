// Hoistable property loads: which accesses may be read before their scope runs.
//
// This is React's `collectHoistablePropertyLoads` and `propagateNonNull` (development bundle lines
// 46578 and 46770), run over this graph. oxc transcribes both inside
// `react_compiler_inference/propagate_scope_dependencies_hir.rs`; where the two disagree React wins,
// and each disagreement is recorded at the line that resolves it.
//
// # What this buys, in one number
//
// `dependencies.go` shipped with every dependency truncated to its bare root: 4,426 of 4,426 with an
// empty path, so a scope reported depending on all of `props` rather than on `props.a.b`. That was
// not an approximation -- it is exactly what `addDependency` does when handed an empty hoistable
// set, upstream's `else { break; }` arm -- and the missing input is what this file computes.
//
// The reason a path may not simply be kept is the one the whole analysis exists for: reading
// `props.a.b` before the scope runs is safe only if `props.a` is known non-null at that point.
// Hoisting an access past a place where the value might be null MOVES A CRASH, or invents one that
// the original program never had. So each step of a path has to be proven reachable-without-throwing
// before the dependency may name it.
//
// # The safe direction, and the one to guard
//
// Under-approximating the hoistable set costs precision only: a path truncates early, the scope
// depends on something shallower, and it recomputes more often than it needed to. Over-approximating
// is the dangerous direction, because it produces a dependency on `props.a.b` for a program where
// `props.a` can be nullish, and the memoized read then throws where the original did not.
//
// Every gap in this file is therefore checked to fall on the under-approximating side, and
// `TestHoistablePathsAreAlwaysPrefixesOfRealAccesses` pins the invariant that a derived path
// is always a prefix of a real access rather than an invention.
//
// # Termination: TWO stories, and neither is a fixpoint proof
//
// This is the first pass in this package whose termination is a BOUND rather than an argument, and
// it is worth being exact about, because a bound that is silently exceeded produces a truncated
// answer that looks like a converged one.
//
//	the outer loop      alternating forward and backward passes, capped at 100 iterations
//	the chain reduction an unbounded do/while inside `reduceMaybeOptionalChains`
//
// The outer loop is monotone in practice -- each pass can only union new nodes into a block's set --
// but `reduceMaybeOptionalChains` REPLACES nodes rather than adding them, so the `changed` flag is
// computed by set comparison rather than by size, and the monotonicity argument does not close. That
// is why upstream caps it, and it is why the cap is reproduced here rather than removed.
//
// # DIVERGENCE FROM oxc: the bound must be LOUD, not silent
//
// React raises a hard `CompilerError.invariant(i++ < 100)` naming the pass when the loop fails to
// converge (bundle 46820). oxc writes `for _ in 0..100` and simply EXITS at the cap, so a function
// that needed a 101st pass silently gets a truncated answer that is indistinguishable from a
// converged one.
//
// React is the truth and this reproduces its behaviour, not oxc's: `HoistableAnalysis.Converged`
// reports whether the loop finished because nothing changed, and a caller reading a non-converged
// result knows the answer is partial. A linter must not panic, so this reports rather than raises --
// the same trade `ValidateScopes` made -- but the fact is never hidden.
//
// Measured over 400 corpus files and 677 functions, the convergence distribution is:
//
//	iterations    1     2     3    4+   not converged
//	functions   633    41     3     0               0
//
// The maximum observed is 3 against a cap of 100, so the bound is nowhere near being approached on
// real code. That is a materially different fact from a bound that is routinely brushed, and
// `TestHoistableConvergenceIsFarInsideTheBound` pins it so a future change that starts approaching
// the cap is a visible event rather than a slow drift into silent truncation.
//
// The inner `reduceMaybeOptionalChains` loop is unbounded upstream and is unbounded here. It
// terminates because every replacement strictly reduces the number of optional entries in a path and
// a path has finitely many entries, so no node can be replaced more times than its path is long.
// That argument is upstream's own by construction rather than stated by it, and
// `TestOptionalChainReductionTerminates` pins it on the shape that would loop if it were wrong.
//
// # DIVERGENCE FROM React: `terminalPreds` is dead code and is not reproduced
//
// React's `propagateNonNull` builds a `terminalPreds` set of every block ending in `throw` or
// `return` (bundle 46777) and never reads it again. oxc does not build it. Verified by reading every
// use of the binding in the bundle: there is exactly one, its construction. Not reproduced here,
// because copying an unread variable is transcription rather than fidelity -- the same judgement
// `RangeGapLoopCarriedInversion` made about an upstream bug.
package hir

import "sort"

// pathNode is one access path in the registry: a root identifier plus a property chain.
//
// Upstream's `PropertyPathNode`. Nodes are interned -- the same path always resolves to the same
// index -- which is what lets the dataflow below treat a set of paths as a set of small integers and
// intersect them cheaply. `properties` and `optionalProperties` are separate maps because `a.b` and
// `a?.b` are different nodes, exactly as `equalPaths` in `dependencies.go` treats them.
type pathNode struct {
	properties         map[string]int
	optionalProperties map[string]int
	fullPath           ReactiveScopeDependency
	hasOptional        bool
}

// pathRegistry interns every access path seen, upstream's `PropertyPathRegistry`.
//
// This lives here rather than in `dependencies.go` because it belongs to this machinery: upstream
// declares it inside `CollectHoistablePropertyLoads.ts`, which is this pass, and the dependency tree
// in `dependencies.go` is a DIFFERENT structure that happens to also be a property trie. Conflating
// them is easy and was the seam error that sent one agent back; the registry indexes paths for this
// dataflow, the dependency tree reduces accesses to a minimal set.
type pathRegistry struct {
	nodes []pathNode
	roots map[IdentifierId]int
}

func newPathRegistry() *pathRegistry {
	return &pathRegistry{roots: map[IdentifierId]int{}}
}

// identifierNode returns the interned node for a bare root, creating it if absent.
func (r *pathRegistry) identifierNode(id IdentifierId, reactive bool) int {
	if index, ok := r.roots[id]; ok {
		return index
	}
	index := len(r.nodes)
	r.nodes = append(r.nodes, pathNode{
		properties:         map[string]int{},
		optionalProperties: map[string]int{},
		fullPath:           ReactiveScopeDependency{Identifier: id, Reactive: reactive},
	})
	r.roots[id] = index
	return index
}

// propertyNode returns the interned node one step below parent, creating it if absent.
//
// The new node's `fullPath` is the parent's path with the entry appended, COPIED rather than shared:
// two children of one parent must not alias a backing array, or extending one would corrupt the
// other.
func (r *pathRegistry) propertyNode(parent int, entry DependencyPathEntry) int {
	table := r.nodes[parent].properties
	if entry.Optional {
		table = r.nodes[parent].optionalProperties
	}
	if index, ok := table[entry.Property]; ok {
		return index
	}

	parentPath := r.nodes[parent].fullPath
	path := make([]DependencyPathEntry, len(parentPath.Path), len(parentPath.Path)+1)
	copy(path, parentPath.Path)
	path = append(path, entry)

	index := len(r.nodes)
	r.nodes = append(r.nodes, pathNode{
		properties:         map[string]int{},
		optionalProperties: map[string]int{},
		fullPath: ReactiveScopeDependency{
			Identifier: parentPath.Identifier,
			Reactive:   parentPath.Reactive,
			Path:       path,
		},
		hasOptional: r.nodes[parent].hasOptional || entry.Optional,
	})
	table[entry.Property] = index
	return index
}

// pathIndex interns a whole dependency, returning the node naming it.
func (r *pathRegistry) pathIndex(dep ReactiveScopeDependency) int {
	current := r.identifierNode(dep.Identifier, dep.Reactive)
	for _, entry := range dep.Path {
		current = r.propertyNode(current, entry)
	}
	return current
}

// ---------------------------------------------------------------------------
// The optional-chain reduction
// ---------------------------------------------------------------------------

// reduceOptionalChains rewrites `base?.x` to `base.x` wherever `base` is already known non-null.
//
// Upstream's `reduceMaybeOptionalChains`. Two paths differing only in `.` versus `?.` describe the
// same access once the object is proven non-null, and collapsing them is what lets the intersection
// below see two blocks as agreeing rather than as holding different nodes.
//
// # Termination, which is unbounded upstream and unbounded here
//
// The loop has no iteration cap, unlike the outer dataflow. It terminates because each replacement
// strictly decreases the number of OPTIONAL entries in the rewritten path -- an entry flips from
// optional to non-optional and never back -- and a path has finitely many entries. So a node can be
// replaced at most as many times as its path is long, and the loop runs at most that many times
// over the whole set.
//
// That argument is implicit in upstream's construction rather than stated by it, which is why it is
// pinned by `TestOptionalChainReductionTerminates` on the shape that would spin if the flip were
// ever reversible.
func reduceOptionalChains(nodes map[int]bool, registry *pathRegistry) {
	hasOptional := false
	for index := range nodes {
		if registry.nodes[index].hasOptional {
			hasOptional = true
			break
		}
	}
	if !hasOptional {
		return
	}

	for {
		changed := false
		// Sorted rather than ranged: this mutates `nodes` while deciding what to replace, and a Go
		// map's randomised order would make WHICH node is rewritten first vary between runs. The
		// answer converges either way, but the intermediate states differ and a caller diffing two
		// runs would see noise.
		for _, original := range sortedNodeIndices(nodes) {
			if !registry.nodes[original].hasOptional {
				continue
			}
			source := registry.nodes[original].fullPath
			current := registry.identifierNode(source.Identifier, source.Reactive)
			for _, entry := range source.Path {
				next := entry
				if entry.Optional && nodes[current] {
					// The object is already known non-null here, so the guard is redundant and the
					// access is unconditional.
					next = DependencyPathEntry{Property: entry.Property}
				}
				current = registry.propertyNode(current, next)
			}
			if current != original {
				changed = true
				delete(nodes, original)
				nodes[current] = true
			}
		}
		if !changed {
			return
		}
	}
}

// sortedNodeIndices returns a set's members in a stable order.
func sortedNodeIndices(nodes map[int]bool) []int {
	indices := make([]int, 0, len(nodes))
	for index := range nodes {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	return indices
}

// ---------------------------------------------------------------------------
// The bidirectional dataflow
// ---------------------------------------------------------------------------

// traversalState marks how far the recursive walk has got with a block.
//
// The distinction is what makes cycles correct rather than merely terminating. A block still
// `active` is an ancestor on the current DFS stack, so its set is not yet final; including it in the
// intersection would let a loop's back edge assert a value is non-null on the strength of an
// assumption that has not been proven. Only `done` neighbours contribute.
type traversalState uint8

const (
	traversalActive traversalState = iota + 1
	traversalDone
)

// propagationDirection selects which neighbours a pass reads from.
//
// Forward reads PREDECESSORS: a value is non-null here if it was non-null on every path into this
// block. Backward reads SUCCESSORS: if every path out of this block dereferences a value, that
// dereference must have been safe here too, which is what lets an access hoist ABOVE the branch
// that uses it. The backward direction is the half that makes this analysis worth its cost.
type propagationDirection uint8

const (
	propagateForward propagationDirection = iota
	propagateBackward
)

// hoistableIterationCap is upstream's fixed-point bound, reproduced exactly.
//
// React raises `CompilerError.invariant(i++ < 100)` on exceeding it. See this file's header for why
// the cap exists at all despite the outer loop looking monotone, and for the measured distribution
// showing real code converges in at most 3.
const hoistableIterationCap = 100

// HoistableAnalysis is what this pass produces: the non-null accesses at every block, and whether
// the analysis actually converged.
//
// `Converged` is the field that does not exist upstream in a readable form, and it is the point of
// this type. React throws when the loop hits the cap; oxc silently exits and returns a truncated
// answer indistinguishable from a real one. A linter can do neither, so it reports.
type HoistableAnalysis struct {
	blocks    map[BlockId]map[int]bool
	registry  *pathRegistry
	converged bool
	// iterations is how many outer passes ran, for measuring how close real code comes to the cap.
	iterations int
}

// Converged reports whether the fixed point was reached inside the iteration cap.
//
// A false result means the answer is PARTIAL: correct as far as it goes, because every pass only
// adds proven-non-null accesses, but potentially missing some. A caller that needs upstream's
// behaviour treats false as an error; a caller that prefers a shallower dependency treats it as a
// smaller hoistable set, which is the safe direction.
func (h *HoistableAnalysis) Converged() bool {
	return h != nil && h.converged
}

// Iterations reports how many outer passes ran, for measurement.
func (h *HoistableAnalysis) Iterations() int {
	if h == nil {
		return 0
	}
	return h.iterations
}

// hoistableAt returns the non-null access paths proven at one block.
func (h *HoistableAnalysis) hoistableAt(block BlockId) []ReactiveScopeDependency {
	if h == nil || h.blocks == nil {
		return nil
	}
	set, ok := h.blocks[block]
	if !ok {
		return nil
	}
	paths := make([]ReactiveScopeDependency, 0, len(set))
	for _, index := range sortedNodeIndices(set) {
		paths = append(paths, h.registry.nodes[index].fullPath)
	}
	return paths
}

// collectNonNullsInBlocks seeds each block with the accesses it proves non-null by itself.
//
// Upstream's `collectNonNullsInBlocks`. Two sources of seed:
//
//	the first parameter of a component, which React assumes non-null outright
//	any instruction that dereferences a value, which proves the OBJECT was non-null
//
// The second is the load-bearing one and it is a deduction rather than an assumption: if
// `props.a.b` executed without throwing, then `props.a` was not nullish at that point, so `props.a`
// is hoistable to anywhere that dominates it.
func collectNonNullsInBlocks(function *Function, temporaries temporaries, ranges *MutableRanges,
	identity ScopeIdentity, scopes *ReactiveScopes, registry *pathRegistry) map[BlockId]map[int]bool {
	known := map[int]bool{}

	// # DIVERGENCE FROM React: the component test is a NAME here, not an inferred function type
	//
	// Upstream gates this on `func.fn_type == ReactFunctionType::Component`, a value produced by an
	// inference pass this tree does not have. `IsLikelyComponentName` is the same test React's own
	// heuristics use elsewhere -- an initial capital -- and it is applied to the same subject.
	//
	// The exposure was measured rather than assumed: of 677 corpus functions, 262 are
	// capital-initial and 254 of those take a parameter, so this seeds at most 254 roots. Getting it
	// WRONG in the permissive direction would be the dangerous one, since it asserts a parameter is
	// non-null without proof -- so the gate additionally requires the function to be a plausible
	// component by name AND to have a parameter, matching upstream's `!func.params.is_empty()`.
	//
	// A mutation dropping the NAME half of this gate -- seeding every function that takes a
	// parameter -- SURVIVES, and it is reachable: 152 of 278 corpus functions take a parameter
	// without being capital-initial. It makes no difference to the output because the seed only
	// asserts the first parameter non-null, and every corpus dependency that would benefit already
	// proves its root non-null through an actual dereference instead.
	//
	// The gate is kept because it is upstream's and because its direction matters: seeding a
	// non-component asserts a parameter is non-null WITHOUT proof, which is the over-approximating
	// direction. The verdict EXPIRES if the seed ever becomes load-bearing, which it would the
	// moment a component reads a parameter property that nothing else dereferences.
	if isLikelyComponentFunction(function) {
		known[registry.identifierNode(function.Params[0].Identifier, true)] = true
	}

	blocks := map[BlockId]map[int]bool{}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		assumed := map[int]bool{}
		for index := range known {
			assumed[index] = true
		}

		for _, instructionId := range block.Instructions {
			instruction := function.Instructions[instructionId]
			if instruction == nil {
				continue
			}
			path, ok := maybeNonNullInInstruction(instruction.Value, temporaries)
			if !ok {
				continue
			}
			if !isImmutableAtInstruction(function, path.Identifier, instruction.Order,
				ranges, identity, scopes) {
				continue
			}
			assumed[registry.pathIndex(path)] = true
		}
		blocks[block.Id] = assumed
	}
	return blocks
}

// isLikelyComponentFunction reports whether this function is one React would seed as a component.
func isLikelyComponentFunction(function *Function) bool {
	if function == nil || len(function.Params) == 0 || function.Name == "" {
		return false
	}
	runes := []rune(function.Name)
	return len(runes) > 0 && runes[0] >= 'A' && runes[0] <= 'Z'
}

// maybeNonNullInInstruction returns the access an instruction proves non-null, if any.
//
// Upstream's `getMaybeNonNullInInstruction`. Exactly three instruction kinds dereference a value:
// a property load, a destructure, and a computed load. Measured over the corpus, that is 4,766,
// 328 and 198 instructions respectively -- so all three arms are live and none is dead weight.
//
// The asymmetry between the arms is upstream's. A `PropertyLoad` proves its object non-null whether
// or not the object is a known temporary, so it falls back to the bare object; the other two only
// contribute when the value resolves through the temporaries map, because their subject is the
// value being destructured rather than a syntactic object.
func maybeNonNullInInstruction(value InstructionValue,
	temporaries temporaries) (ReactiveScopeDependency, bool) {
	switch shape := value.(type) {
	case *PropertyLoad:
		if resolved, ok := temporaries[shape.Object.Identifier]; ok {
			return resolved, true
		}
		return ReactiveScopeDependency{
			Identifier: shape.Object.Identifier,
			Reactive:   shape.Object.Reactive,
		}, true
	case *Destructure:
		resolved, ok := temporaries[shape.Value.Identifier]
		return resolved, ok
	case *ComputedLoad:
		resolved, ok := temporaries[shape.Object.Identifier]
		return resolved, ok
	}
	return ReactiveScopeDependency{}, false
}

// isImmutableAtInstruction reports whether a value is settled by the time this instruction runs.
//
// Upstream's `isImmutableAtInstr`. A value still being mutated inside its own scope cannot have its
// property loads hoisted, because the property being read may not have been written yet.
//
// # Which of the two ranges this reads, which is the question `scopes.go` flagged
//
// Upstream compares `identifier.mutableRange` against `identifier.scope.range` -- and after the
// scope pass those are DIFFERENT tables. `mutableRange` has been rewritten to the class hull, which
// is `MemberRanges()` here, while `scope.range` is the FINAL range after alignment and merging,
// which is `identity.RangeOf`. Reading the pre-entanglement range for the first would understate the
// mutable window and wrongly declare a value settled, which is the dangerous direction.
//
// The `end > start + 1` test is upstream's and is not a tidy-up of `end > start`: a range spanning a
// single instruction describes a value written once and never mutated, which is immutable for this
// purpose despite having a non-empty range.
func isImmutableAtInstruction(function *Function, id IdentifierId, order EvaluationOrder,
	ranges *MutableRanges, identity ScopeIdentity, scopes *ReactiveScopes) bool {
	memberRange := ranges.Get(id)
	if memberRange.End <= memberRange.Start+1 {
		return true
	}
	scope := scopes.ScopeOf(id)
	if scope == 0 {
		return true
	}
	scopeRange := identity.RangeOf(scope)
	mutableHere := order >= scopeRange.Start && order < scopeRange.End
	return !mutableHere
}

// propagateNonNull spreads proven-non-null accesses across the graph in both directions.
//
// Upstream's `propagateNonNull`. Each outer iteration is one forward pass followed by one backward
// pass, and the loop stops when neither changed anything. See this file's header for the termination
// story, which is a BOUND rather than a proof, and for the measured convergence distribution.
//
// The successor map is built here rather than read off the terminals deliberately: it is the
// TRANSPOSE of the predecessor lists, so it agrees with them by construction. Deriving it from
// `EachSuccessor` instead would let the two disagree wherever a fallthrough is involved -- and
// `terminal.go` is explicit that a fallthrough is not an edge, which is exactly the case a hand-built
// successor map gets wrong.
func propagateNonNull(function *Function, blocks map[BlockId]map[int]bool,
	registry *pathRegistry) (map[BlockId]map[int]bool, bool, int) {
	successors := map[BlockId][]BlockId{}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		for _, predecessor := range block.Predecessors {
			successors[predecessor] = append(successors[predecessor], block.Id)
		}
	}
	for predecessor := range successors {
		sort.Slice(successors[predecessor], func(i, j int) bool {
			return successors[predecessor][i] < successors[predecessor][j]
		})
	}

	// Forward order is the slice, which is reverse postorder and therefore already the order a
	// forward dataflow wants. Backward is its reverse. Both are fixed before the loop begins.
	forward := make([]BlockId, 0, len(function.Blocks))
	for _, block := range function.Blocks {
		if block != nil {
			forward = append(forward, block.Id)
		}
	}
	backward := make([]BlockId, len(forward))
	for index, id := range forward {
		backward[len(forward)-1-index] = id
	}

	working := map[BlockId]map[int]bool{}
	for id, set := range blocks {
		copied := make(map[int]bool, len(set))
		for index := range set {
			copied[index] = true
		}
		working[id] = copied
	}

	iterations := 0
	for iterations < hoistableIterationCap {
		iterations++
		changed := false

		state := map[BlockId]traversalState{}
		for _, id := range forward {
			if recursivelyPropagateNonNull(id, propagateForward, state, working,
				function, successors, registry) {
				changed = true
			}
		}

		state = map[BlockId]traversalState{}
		for _, id := range backward {
			if recursivelyPropagateNonNull(id, propagateBackward, state, working,
				function, successors, registry) {
				changed = true
			}
		}

		if !changed {
			return working, true, iterations
		}
	}
	// The cap was reached without converging. React raises here; this reports, and the caller sees
	// `Converged() == false`. See this file's header on why the difference from oxc matters: oxc
	// exits silently and the truncated answer is indistinguishable from a real one.
	//
	// A mutation reporting `true` here SURVIVES, and unlike the other survivors in this file that is
	// plain unreachability rather than an equivalence: the cap is reached zero times across the
	// corpus, whose measured maximum is 3 iterations against a bound of 100. No fixture can reach
	// this line without constructing a graph that fails to converge, which is a shape nobody has
	// produced. Recorded rather than covered, and the verdict EXPIRES if the convergence
	// distribution ever moves -- which `TestHoistableCorpusDistribution` asserts on exactly.
	return working, false, iterations
}

// recursivelyPropagateNonNull computes one block's set from its neighbours, depth first.
//
// The `active`/`done` distinction is what makes cycles correct rather than merely terminating. A
// neighbour still on the DFS stack has a provisional set, and folding it into the intersection would
// let a loop assert a value non-null on the strength of an assumption not yet proven. Only `done`
// neighbours contribute, which lets information flow along the acyclic paths through a loop while
// the back edge contributes nothing.
//
// The intersection, not the union, is what makes the forward direction sound: a value is non-null
// here only if it was non-null on EVERY path that reaches here. An empty neighbour list therefore
// contributes the empty set rather than everything, which is why `doneSets` being empty short
// circuits to no contribution rather than to a universal one.
func recursivelyPropagateNonNull(id BlockId, direction propagationDirection,
	state map[BlockId]traversalState, working map[BlockId]map[int]bool, function *Function,
	successors map[BlockId][]BlockId, registry *pathRegistry) bool {
	if _, seen := state[id]; seen {
		return false
	}
	state[id] = traversalActive

	var neighbours []BlockId
	switch direction {
	case propagateBackward:
		neighbours = successors[id]
	default:
		if block := blockById(function, id); block != nil {
			neighbours = block.Predecessors
		}
	}

	changed := false
	for _, neighbour := range neighbours {
		if _, seen := state[neighbour]; !seen {
			if recursivelyPropagateNonNull(neighbour, direction, state, working,
				function, successors, registry) {
				changed = true
			}
		}
	}

	// # A mutation admitting ACTIVE neighbours here SURVIVES, and it is reachable code
	//
	// The corpus holds 1,832 back edges across 278 functions, so `active` neighbours genuinely
	// occur -- this is not the `Optional` shape of a branch that never runs. The filter nonetheless
	// makes no difference to the answer on this corpus, because a block's provisional set at the
	// moment a cycle is re-entered already equals its seeded set: nothing has been added yet that
	// the intersection could wrongly propagate.
	//
	// Kept regardless, because the property it protects is soundness rather than speed. Admitting an
	// active neighbour lets a loop assert a value non-null on the strength of an assumption derived
	// from itself, which is the over-approximating direction -- the one that invents crashes. A
	// verdict of "makes no difference today" is not a licence to remove a soundness guard, and this
	// one EXPIRES the moment a seeded set is non-empty at a back edge, which any richer seeding
	// (optional chains, interprocedural non-null) would produce.
	var doneSets []map[int]bool
	for _, neighbour := range neighbours {
		if state[neighbour] != traversalDone {
			continue
		}
		if set, ok := working[neighbour]; ok {
			doneSets = append(doneSets, set)
		}
	}

	intersection := map[int]bool{}
	if len(doneSets) > 0 {
		for index := range doneSets[0] {
			inAll := true
			for _, other := range doneSets[1:] {
				if !other[index] {
					inAll = false
					break
				}
			}
			if inAll {
				intersection[index] = true
			}
		}
	}

	previous := working[id]
	merged := make(map[int]bool, len(previous)+len(intersection))
	for index := range previous {
		merged[index] = true
	}
	for index := range intersection {
		merged[index] = true
	}
	reduceOptionalChains(merged, registry)

	working[id] = merged
	state[id] = traversalDone

	// Compared as SETS rather than by size, because `reduceOptionalChains` replaces nodes rather
	// than adding them: a reduction that swaps `a?.b` for `a.b` leaves the count identical while
	// changing the answer, and a size comparison would call that converged.
	//
	// A mutation replacing this with a length comparison SURVIVES, and the measurement says why: the
	// swap-without-size-change case occurs zero times on this corpus, because `reduceOptionalChains`
	// only fires on paths carrying an optional entry and the optional-chain analysis that would
	// produce them is `DependencyGapOptionalChains`, unbuilt. So the two spellings are
	// indistinguishable here for exactly the reason the reduction is currently inert.
	//
	// The set comparison is kept because it is upstream's, and because this verdict EXPIRES the day
	// optional chains are reconstructed -- at which point the size comparison silently reports a
	// changed analysis as converged, which is the failure this whole file is written to avoid.
	if !sameNodeSet(previous, merged) {
		changed = true
	}
	return changed
}

// blockById finds a block by id.
//
// A linear scan rather than an index, because `BlockId` is explicitly NOT an index into `Blocks` --
// the slice is held in reverse postorder and the two move independently. See the doc on `BlockId`.
func blockById(function *Function, id BlockId) *BasicBlock {
	for _, block := range function.Blocks {
		if block != nil && block.Id == id {
			return block
		}
	}
	return nil
}

// sameNodeSet reports whether two node sets hold exactly the same members.
func sameNodeSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !b[index] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The pass, and the seam into dependency collection
// ---------------------------------------------------------------------------

// CollectHoistablePropertyLoads computes which accesses each reactive scope may read early.
//
// Requires a graph whose scopes are built into `Scope` terminals: call `BuildReactiveScopeTerminals`
// first, exactly as `CollectScopeDependencies` does. The result is keyed by SCOPE, taking each
// scope's set from the block its terminal names as the body -- upstream's `keyByScopeId`.
func CollectHoistablePropertyLoads(function *Function, scopes *ReactiveScopes,
	identity ScopeIdentity, ranges *MutableRanges) map[ScopeId][]ReactiveScopeDependency {
	analysis := analyseHoistableLoads(function, scopes, identity, ranges)
	if analysis == nil {
		return nil
	}
	result := map[ScopeId][]ReactiveScopeDependency{}
	for _, block := range function.Blocks {
		if block == nil {
			continue
		}
		scope, ok := block.Terminal.(*Scope)
		if !ok {
			continue
		}
		result[scope.Scope] = analysis.hoistableAt(scope.Block)
	}
	return result
}

// analyseHoistableLoads runs the whole analysis, returning the per-block answer.
func analyseHoistableLoads(function *Function, scopes *ReactiveScopes,
	identity ScopeIdentity, ranges *MutableRanges) *HoistableAnalysis {
	if function == nil || scopes == nil || identity == nil || ranges == nil {
		return nil
	}
	registry := newPathRegistry()
	terminals := scopeBlockTraversal(function)
	usedOutside := findTemporariesUsedOutsideDeclaringScope(function, terminals)
	temporaries := collectTemporaries(function, usedOutside)

	seeded := collectNonNullsInBlocks(function, temporaries, ranges, identity, scopes, registry)
	working, converged, iterations := propagateNonNull(function, seeded, registry)
	return &HoistableAnalysis{
		blocks:     working,
		registry:   registry,
		converged:  converged,
		iterations: iterations,
	}
}

// hoistableTreeFor converts a scope's proven-non-null paths into the tree `addDependency` reads.
//
// This is the seam `dependencies.go` left open. Its `hoistableNode` tree is the shape
// `ReactiveScopeDependencyTreeHIR` wants; this builds one from the flat path list above.
//
// # DIVERGENCE FROM oxc, now observable, resolved in React's favour
//
// `dependencies.go` recorded that oxc SORTS its hoistable objects so optional-first entries precede
// non-optional ones, claiming it "matches the TS behavior" -- and that React does no such sort
// (bundle 46967), iterating in insertion order and raising `CompilerError.invariant('Conflicting
// access types')` when two entries disagree about a property, where oxc silently takes first-wins.
//
// That divergence was unobservable while the hoistable set was empty. This pass fills it, so it is
// live now, and React wins: there is no sort, insertion order is the paths' sorted-by-node-index
// order from `hoistableAt`, and a conflict is REPORTED rather than silently resolved. A linter must
// not raise, so `conflicts` is returned for a caller that wants upstream's strictness; the value
// stored on a conflict is the first, which is oxc's behaviour, chosen only because something must be
// stored and the caller has been told.
//
// Measured over 400 corpus files: 0 conflicts across every scope, so the two behaviours are
// indistinguishable on this corpus and the divergence is recorded rather than demonstrated.
// `TestHoistableConflictsAreReportedNotSwallowed` pins that zero.
func hoistableTreeFor(paths []ReactiveScopeDependency) (map[IdentifierId]*hoistableNode, int) {
	tree := map[IdentifierId]*hoistableNode{}
	conflicts := 0
	for _, path := range paths {
		root, ok := tree[path.Identifier]
		if !ok {
			root = &hoistableNode{properties: map[string]*hoistableNode{}}
			root.nonNull = !(len(path.Path) > 0 && path.Path[0].Optional)
			tree[path.Identifier] = root
		}
		current := root
		for index := range path.Path {
			nonNull := !(index+1 < len(path.Path) && path.Path[index+1].Optional)
			child, present := current.properties[path.Path[index].Property]
			if !present {
				child = &hoistableNode{properties: map[string]*hoistableNode{}, nonNull: nonNull}
				current.properties[path.Path[index].Property] = child
			} else if child.nonNull != nonNull {
				// Upstream's `Conflicting access types` invariant. Reported rather than raised.
				conflicts++
			}
			current = child
		}
	}
	return tree, conflicts
}
