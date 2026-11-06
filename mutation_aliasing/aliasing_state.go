package mutation_aliasing

import "github.com/system-inc/cohere/static_single_assignment"

// aliasingState is the directed edge graph the widening walks.
//
// # This is a graph pass, not a per-instruction rule, and that was verified rather than assumed
//
// Stage 2's doc comment above describes `AliasingState.mutate` as "one function fed from
// `instr.effects`", which is accurate about its input and understates its structure. It was checked
// against both implementations directly rather than taken on trust, because two agents had
// described it differently and only one had read it.
//
// What both implementations actually contain, at React 42186-42280 and oxc 213-393: a node type
// carrying `created_from`, `captures`, `aliases`, `maybe_aliases` and a forward `edges` list; four
// builders (`create`, `create_from`/`assign`, `capture`, `maybe_alias`) called from the ranges
// pass's own main loop; and `mutate` as a worklist over that graph. The single widening write in the
// entire upstream file, `ident.mutable_range.end.max(end_val)`, is inside that worklist. So the
// characterization is right and the graph is not incidental: remove it and the widening reaches
// only the directly-named value, which is the counterfactual measured in this stage's report and it
// is strictly weaker than upstream.
type aliasingState struct {
	nodes map[static_single_assignment.IdentifierId]*aliasingNode

	// identities tracks the identifiers that currently denote the same abstract value.
	//
	// Upstream stores a set of InstructionValues for every identifier. An Assign of a mutable
	// value points the destination at the source's existing set, so a later Freeze through either
	// name changes the kind observed through every other name. The range graph's alias edges are
	// not an adequate substitute: Alias and MaybeAlias edges describe possible information flow,
	// not shared abstract identity, and freezing across them would suppress real mutations.
	//
	// This reduced pass needs only the single-value Assign case. Each Create starts a fresh identity;
	// Assign moves its destination into the source identity while CreateFrom deliberately retains a
	// distinct identity with a copied kind.
	identities      map[static_single_assignment.IdentifierId]uint64
	identityMembers map[uint64]map[static_single_assignment.IdentifierId]struct{}
	nextIdentity    uint64

	// freezeSources are values upstream's abstract value itself points through for freezing. A phi
	// denotes the union of its operands, and freezing a FunctionExpression recursively freezes its
	// captures. These are deliberately separate from the range graph's Capture edges: an object can
	// capture a value without Freeze(object) recursively freezing that value.
	freezeSources map[static_single_assignment.IdentifierId][]static_single_assignment.IdentifierId

	// immutable is upstream's abstract value kind, reduced to the one bit the mutation gate needs.
	//
	// `InferMutationAliasingEffects` carries a six-point lattice -- Mutable, Context, Primitive,
	// Frozen, MaybeFrozen, Global -- because it uses the distinctions to phrase diagnostics. The
	// only consumer here is `state.mutate` at `InferMutationAliasingEffects.ts:1489`, whose
	// conditional arm mutates for `Mutable` and `Context` and returns `none` for everything else.
	// So one bit is the whole answer: present means "not Mutable or Context", absent means mutable.
	//
	// Absent rather than false is deliberate. A value the walk never reached has no entry, and the
	// gate treats that as mutable, which is the pre-existing behaviour of this pass and the
	// conservative direction for widening.
	immutable map[static_single_assignment.IdentifierId]EffectValueKind
}

// markImmutable records that a value is not Mutable or Context, so a conditional mutation of it
// widens nothing.
func (state *aliasingState) markImmutable(id static_single_assignment.IdentifierId, kind EffectValueKind) {
	state.immutable[id] = kind
}

// freeze marks an already-known abstract value as frozen.
//
// Upstream's InferenceState.freeze starts by looking up kind(place); it changes the instruction
// values that place currently denotes, but it never creates a value for a place the state has not
// initialized. This reduced state has no instruction-value sets, so node presence is its
// initialization predicate. In particular, retaining an immutable bit for an absent node would
// make a later Assign from that unresolved place frozen retroactively.
func (state *aliasingState) freeze(id static_single_assignment.IdentifierId) bool {
	return state.freezeWithSeen(id, map[static_single_assignment.IdentifierId]bool{})
}

// freezeWithSeen is freeze, carrying the values this walk has already frozen so a cycle of sources ends.
//
// Mutates: seen each value visited is added, so the recursive calls share one record of the walk
func (state *aliasingState) freezeWithSeen(
	id static_single_assignment.IdentifierId,
	seen map[static_single_assignment.IdentifierId]bool,
) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	if _, initialized := state.nodes[id]; !initialized {
		return false
	}
	if kind, immutable := state.immutable[id]; immutable && kind != EffectValueMaybeFrozen {
		return false
	}
	identity, shared := state.identities[id]
	if !shared {
		state.markImmutable(id, EffectValueFrozen)
		for _, source := range state.freezeSources[id] {
			state.freezeWithSeen(source, seen)
		}
		return true
	}
	for alias := range state.identityMembers[identity] {
		state.markImmutable(alias, EffectValueFrozen)
		for _, source := range state.freezeSources[alias] {
			state.freezeWithSeen(source, seen)
		}
	}
	return true
}

// notMutable reports whether a value is neither Mutable nor Context.
//
// The question `state.mutate` asks: its conditional arm mutates for `Mutable` and `Context` and
// returns `none` for everything else (`InferMutationAliasingEffects.ts:1489`). Absent means mutable,
// which is the conservative direction for widening and the pre-existing behaviour of this pass.
func (state *aliasingState) notMutable(id static_single_assignment.IdentifierId) bool {
	_, present := state.immutable[id]
	return present
}

// deriveImmutable gives Into the same mutability as From.
//
// Upstream's `CreateFrom` at `InferMutationAliasingEffects.ts:731` reads the source's kind and
// initializes the target with it, rewriting the effect into a plain `Create` of that kind when the
// source is Primitive, Global or Frozen. This is that propagation, in the one bit this pass reads.
func (state *aliasingState) deriveImmutable(
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	if kind, present := state.immutable[from]; present {
		state.immutable[into] = kind
		return
	}
	delete(state.immutable, into)
}

// create makes a node, replacing any existing one.
//
// Replacement rather than merge is upstream's: `self.nodes.insert(...)` in Rust and
// `this.nodes.set(...)` in JavaScript both overwrite. A value redefined by a later instruction gets
// a fresh node with no edges, which is correct in single-assignment form because the redefinition is
// a different value.
func (state *aliasingState) create(id static_single_assignment.IdentifierId, value aliasingNodeValue) {
	state.nodes[id] = newAliasingNode(id, value)
	delete(state.immutable, id)
	delete(state.freezeSources, id)
	state.detachIdentity(id)
	state.nextIdentity++
	state.identities[id] = state.nextIdentity
	state.identityMembers[state.nextIdentity] = map[static_single_assignment.IdentifierId]struct{}{id: {}}
}

// recordFreezeSources gives a phi or FunctionExpression the values React freezes through when
// that abstract value is frozen later.
//
// The builder hands the sources over as a slice of their own, which the state keeps.
func (state *aliasingState) recordFreezeSources(
	into static_single_assignment.IdentifierId,
	sources []static_single_assignment.IdentifierId,
) {
	if len(sources) == 0 {
		return
	}
	state.freezeSources[into] = sources
}

// shareIdentity makes into denote the same abstract value as from.
//
// It is called only for Assign from a mutable/context source. Alias, Capture and MaybeAlias must
// not call it: upstream records those as effects without changing the InstructionValues either
// identifier denotes.
func (state *aliasingState) shareIdentity(
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	if from == into {
		return
	}
	identity, ok := state.identities[from]
	if !ok {
		return
	}
	state.detachIdentity(into)
	state.identities[into] = identity
	state.identityMembers[identity][into] = struct{}{}
}

// detachIdentity removes one identifier from its previous abstract value without disturbing the
// aliases that still denote it. A later Create or Assign is a new definition of this identifier.
func (state *aliasingState) detachIdentity(id static_single_assignment.IdentifierId) {
	identity, ok := state.identities[id]
	if !ok {
		return
	}
	delete(state.identities, id)
	members := state.identityMembers[identity]
	delete(members, id)
	if len(members) == 0 {
		delete(state.identityMembers, identity)
	}
}

// createFrom is the `CreateFrom` effect: Into is a new value derived from From.
//
// Upstream creates the target node unconditionally first, which discards any edges it had. Then it
// adds a forward alias edge from -> into and records `created_from` on the target. Note the
// asymmetry with `assign`: `create_from` does not check that the source node exists, because the
// target is being created here regardless.
func (state *aliasingState) createFrom(
	index int,
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	state.create(into, aliasingNodeObject)
	if fromNode, ok := state.nodes[from]; ok {
		fromNode.edges = append(fromNode.edges, aliasingEdge{index: index, node: into, kind: aliasingEdgeAlias})
	}
	if toNode, ok := state.nodes[into]; ok {
		insertBackEdge(toNode.createdFrom, &toNode.createdFromOrder, from, index)
	}
}

// assign is the `Assign` and `Alias` effects: mutating Into implies mutating From.
//
// Both endpoints must already exist or the edge is dropped entirely. That guard is upstream's and it
// is the reason the entry values (params, context, returns) are created before the walk begins:
// without those nodes, every edge touching a parameter would be silently discarded.
func (state *aliasingState) assign(
	index int,
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	fromNode, fromOk := state.nodes[from]
	toNode, toOk := state.nodes[into]
	if !fromOk || !toOk {
		return
	}
	fromNode.edges = append(fromNode.edges, aliasingEdge{index: index, node: into, kind: aliasingEdgeAlias})
	insertBackEdge(toNode.aliases, &toNode.aliasesOrder, from, index)
}

// capture is the `Capture` effect: information flows from From into Into without aliasing.
//
// A capture edge is followed backwards only by a transitive mutation. Mutating a container does not
// mutate what it holds; mutating it transitively does.
func (state *aliasingState) capture(
	index int,
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	fromNode, fromOk := state.nodes[from]
	toNode, toOk := state.nodes[into]
	if !fromOk || !toOk {
		return
	}
	fromNode.edges = append(fromNode.edges, aliasingEdge{index: index, node: into, kind: aliasingEdgeCapture})
	insertBackEdge(toNode.captures, &toNode.capturesOrder, from, index)
}

// maybeAlias is the `MaybeAlias` effect: a possible aliasing relationship, from an unknown callee.
//
// Traversing this edge in either direction downgrades the mutation to Conditional, which is the
// conservative direction: an uncertain alias must not turn into a definite mutation.
func (state *aliasingState) maybeAlias(
	index int,
	from static_single_assignment.IdentifierId,
	into static_single_assignment.IdentifierId,
) {
	fromNode, fromOk := state.nodes[from]
	toNode, toOk := state.nodes[into]
	if !fromOk || !toOk {
		return
	}
	fromNode.edges = append(fromNode.edges, aliasingEdge{index: index, node: into, kind: aliasingEdgeMaybeAlias})
	insertBackEdge(toNode.maybeAliases, &toNode.maybeAliasOrder, from, index)
}

// mutate walks the alias graph from one mutated value, widening every range it reaches.
//
// This is React 42188-42280 and oxc 213-393, and it is the only function in either implementation
// that writes a wider `end`. Every other range write opens a range at its definition point.
//
// # The termination argument, stated rather than assumed
//
// This is a worklist over a graph with a widening that takes a max, which is the shape that fails to
// terminate when the revisit test is wrong. Three passes in this tree have already needed
// hand-written equality for related reasons, and the failure mode here is silent: a comparison that
// includes anything varying meaninglessly between rounds spins forever without failing loudly.
//
// The termination is not the widening reaching a fixpoint, and reading it that way is the trap. The
// widening is a side effect of the walk. What bounds the walk is the `seen` map, keyed by identifier
// and valued by `mutationKind`, with the test `previous >= current -> skip`.
//
// The argument in full:
//
//   - `seen` maps each identifier to the strongest kind it has been visited with.
//   - A node is processed only when its kind is strictly greater than what `seen` holds, and
//     processing immediately writes that greater kind back.
//   - `mutationKind` has exactly two values, Conditional (1) and Definite (2), and the queue never
//     pushes a kind not derived from an existing one -- an edge either preserves the kind or
//     downgrades it to Conditional.
//   - So each identifier can be processed at most twice: once at Conditional, once at Definite.
//     Never more, because the second Definite visit finds `previous >= current` and skips.
//   - Identifiers in one function are finite. Therefore the loop runs at most 2N times and halts.
//
// The bound is on the number of processed entries, not on the queue length: the queue can hold
// duplicates, and they are discarded cheaply at pop. That is upstream's structure exactly.
//
// Two consequences worth naming because they are the difference between reproducing this and
// writing something that merely looks like it:
//
// The `seen` map is per-call, not per-pass. Each mutation starts a fresh walk. So a value reached by
// two different mutations is widened twice, once per call, which is exactly what makes `end` grow to
// the last mutation rather than the first. A `seen` map hoisted out to the pass level would look
// like a sensible optimization and would silently truncate every range to its first mutation.
//
// The comparison is on `mutationKind` alone, which is a two-element enum. It deliberately does not
// include the direction, the transitive flag, or the range value. Including the range would be the
// convergence-equality failure this tree has hit before: `end` grows monotonically, so a test that
// re-queued whenever `end` moved would still terminate, but including the direction would not
// terminate on a graph with an alias cycle, since a node can be legitimately revisited in the other
// direction forever.
//
// # Why the index test is what keeps this sound rather than merely finite
//
// Every edge and mutation carries a sequence index assigned as the graph was built. The walk skips
// any edge whose index is at or after the mutation's own index, so a mutation only flows through
// aliases that already existed when it happened. That is upstream's model of time and it is why a
// value aliased after being mutated does not retroactively widen.
//
// Upstream's forward-edge loop uses `break` rather than `continue` on that test, which is correct
// only because forward edges are appended in index order and therefore sorted. Reproduced as `break`
// with the sortedness noted here, because a `continue` would be a silent behavior change on any
// graph where the assumption failed.
func (state *aliasingState) mutate(
	ranges *MutableRanges,
	index int,
	start static_single_assignment.IdentifierId,
	end static_single_assignment.EvaluationOrder,
	hasEnd bool,
	transitive bool,
	startKind mutationKind,
) {
	seen := map[static_single_assignment.IdentifierId]mutationKind{}
	queue := []mutationQueueEntry{{
		place:      start,
		transitive: transitive,
		direction:  mutationDirectionBackwards,
		kind:       startKind,
	}}

	for len(queue) > 0 {
		entry := queue[len(queue)-1]
		queue = queue[:len(queue)-1]

		if previous, ok := seen[entry.place]; ok && previous >= entry.kind {
			continue
		}
		seen[entry.place] = entry.kind

		node, ok := state.nodes[entry.place]
		if !ok {
			continue
		}

		// The widening. This is the whole point of the pass, and it is a max rather than an
		// assignment because a value mutated at several instructions keeps the last one.
		if hasEnd {
			existing := ranges.Get(node.id)
			if end > existing.End {
				existing.End = end
				ranges.Set(node.id, existing)
			}
		}

		// Forward edges: mutating a value mutates what it was aliased or captured into.
		for _, edge := range node.edges {
			if edge.index >= index {
				// Sorted by construction; see the note on `break` in the doc comment.
				break
			}
			kind := entry.kind
			if edge.kind == aliasingEdgeMaybeAlias {
				kind = mutationKindConditional
			}
			queue = append(queue, mutationQueueEntry{
				place:      edge.node,
				transitive: entry.transitive,
				direction:  mutationDirectionForwards,
				kind:       kind,
			})
		}

		// `createdFrom` always forces transitive, whatever the incoming entry said. Mutating a value
		// derived from another reaches everything that other value transitively holds.
		//
		// This path is live: PropertyLoad and ordinary Destructure bindings emit CreateFrom. Forcing
		// transitive to false loses mutations of values captured inside the source; the focused
		// CreateFrom test pins that distinction rather than merely checking the source itself widens.
		for _, alias := range node.createdFromOrder {
			if node.createdFrom[alias] >= index {
				continue
			}
			queue = append(queue, mutationQueueEntry{
				place:      alias,
				transitive: true,
				direction:  mutationDirectionBackwards,
				kind:       entry.kind,
			})
		}

		// Backward alias edges, suppressed for a phi reached travelling forwards.
		if entry.direction == mutationDirectionBackwards || node.value != aliasingNodePhi {
			for _, alias := range node.aliasesOrder {
				if node.aliases[alias] >= index {
					continue
				}
				queue = append(queue, mutationQueueEntry{
					place:      alias,
					transitive: entry.transitive,
					direction:  mutationDirectionBackwards,
					kind:       entry.kind,
				})
			}
			for _, alias := range node.maybeAliasOrder {
				if node.maybeAliases[alias] >= index {
					continue
				}
				queue = append(queue, mutationQueueEntry{
					place:      alias,
					transitive: entry.transitive,
					direction:  mutationDirectionBackwards,
					kind:       mutationKindConditional,
				})
			}
		}

		// Captures are followed backwards only by a transitive mutation. Mutating a container does
		// not mutate what it holds; mutating it transitively does.
		if entry.transitive {
			for _, capture := range node.capturesOrder {
				if node.captures[capture] >= index {
					continue
				}
				queue = append(queue, mutationQueueEntry{
					place:      capture,
					transitive: entry.transitive,
					direction:  mutationDirectionBackwards,
					kind:       entry.kind,
				})
			}
		}
	}
}
