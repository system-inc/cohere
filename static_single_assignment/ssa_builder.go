package static_single_assignment

import "math"

type ssaBuilder[G Graph[F, B, P], F any, B comparable, P any] struct {
	graph    G
	function F

	// states, unsealedPreds and visited are by BlockId, dense: a block id indexes them directly, where a map
	// would hash it on every lookup Braun's walk makes (#beapx0q). They hold BlockBound entries, and grow if
	// an id past it ever comes, so no id is out of reach. A nil state is a block not yet touched.
	states []*ssaState[P]

	// unsealedPreds counts, per block, how many predecessors have not yet been processed. A block
	// whose count reaches zero is sealed and its incomplete phis can be filled.
	//
	// unseenPredecessors means "not yet decremented", which is not the same as zero; the read sites
	// initialise from the predecessor count on first touch for exactly that reason.
	unsealedPreds []int

	// unknown holds bindings a lookup walked off the entry block without finding. In cohere they are
	// globals, or captures from an enclosing function; in Adamic, which keeps both out of its graph, a
	// variable read before anything defines it. They are left un-renamed: there is no definition in
	// this function to merge, so a phi over them would be an invention. Made on its first entry, since most
	// functions have none.
	unknown map[DeclarationId]bool

	// visited records blocks already processed, so sealing only fills phis for a block whose body
	// has actually been walked.
	visited []bool

	// walking, walkingBlock and walkingContextStore are where run's walk is, read by visitPlace.
	walking             walking
	walkingBlock        BlockId
	walkingContextStore bool
}

// unseenPredecessors marks a block's unsealedPreds entry as not yet touched. It is no count a decrement can reach,
// so a block decremented past zero still reads as touched, as an entry in the map it replaces did.
const unseenPredecessors = math.MinInt

// makeDenseState sizes the builder's per-block state to count blocks, every block untouched.
func (builder *ssaBuilder[G, F, B, P]) makeDenseState(count int) {
	builder.states = make([]*ssaState[P], count)
	builder.visited = make([]bool, count)
	builder.unsealedPreds = make([]int, count)
	for index := range builder.unsealedPreds {
		builder.unsealedPreds[index] = unseenPredecessors
	}
}

// reach grows the builder's per-block state to hold blockId, when an id at or past the bound Graph.BlockBound
// gave comes; the new blocks are untouched.
func (builder *ssaBuilder[G, F, B, P]) reach(blockId BlockId) {
	for int(blockId) >= len(builder.states) {
		builder.states = append(builder.states, nil)
		builder.visited = append(builder.visited, false)
		builder.unsealedPreds = append(builder.unsealedPreds, unseenPredecessors)
	}
}

// stateAt is blockId's state, made on its first need.
func (builder *ssaBuilder[G, F, B, P]) stateAt(blockId BlockId) *ssaState[P] {
	builder.reach(blockId)
	if builder.states[blockId] == nil {
		builder.states[blockId] = newSsaState[P]()
	}
	return builder.states[blockId]
}

// existingState is blockId's state, and false when nothing has touched the block yet.
func (builder *ssaBuilder[G, F, B, P]) existingState(blockId BlockId) (*ssaState[P], bool) {
	if int(blockId) >= len(builder.states) || builder.states[blockId] == nil {
		return nil, false
	}
	return builder.states[blockId], true
}

// remainingPredecessors is how many of blockId's predecessors are unprocessed, reading count, the block's
// predecessor total, when nothing has decremented it yet.
func (builder *ssaBuilder[G, F, B, P]) remainingPredecessors(blockId BlockId, count int) int {
	builder.reach(blockId)
	if remaining := builder.unsealedPreds[blockId]; remaining != unseenPredecessors {
		return remaining
	}
	return count
}

func (builder *ssaBuilder[G, F, B, P]) run() {
	builder.makeDenseState(builder.graph.BlockBound(builder.function))

	// Parameters are definitions in the entry block. Renaming them is what makes a reassigned
	// parameter behave like any other binding rather than like a global.
	entryId := builder.graph.Entry(builder.function)
	if _, ok := builder.graph.Block(builder.function, entryId); !ok {
		return
	}
	builder.reach(entryId)
	builder.states[entryId] = newSsaState[P]()
	parameters := builder.graph.Parameters(builder.function)
	for index := range parameters {
		builder.defineIn(entryId, &parameters[index])
	}

	// One visitor for every place, made once with the builder, which reads what to do from
	// b.walking. A visitor handed to a Graph method escapes, since the compiler cannot see through a
	// type parameter's method, and so does every local it captures; one per instruction would be an
	// allocation per instruction.
	visit := func(place *P, role Role) { builder.visitPlace(place, role) }
	edges := takeEdgeScratch()
	defer edgeScratchPool.Put(edges)

	for _, block := range builder.graph.Blocks(builder.function) {
		blockId := builder.graph.Id(block)
		builder.walkingBlock = blockId
		builder.stateAt(blockId)
		builder.visited[blockId] = true

		count := builder.graph.InstructionCount(builder.function, block)
		for index := 0; index < count; index++ {
			// Uses first, then definitions. `x = x + 1` must read the old x before the store mints
			// the new one; visiting in the other order would make the increment read itself.
			builder.walking = walkingUses
			builder.graph.EachInstructionPlace(builder.function, block, index, visit)
			// Only a context store reuses its binding's definition. The guard keys on the
			// declaration, and a `FunctionExpression` assigned to the same declaration would
			// otherwise reuse the identifier too -- measured as a duplicate definition of a
			// function value on `let n = 1; const g = () => n; n = 2`.
			builder.walking = walkingDefinitions
			builder.walkingContextStore = builder.graph.IsContextStore(builder.function, block, index)
			builder.graph.EachInstructionPlace(builder.function, block, index, visit)
		}

		builder.walking = walkingTerminal
		builder.graph.EachTerminalPlace(block, visit)

		// Seal each successor that this block was the last unprocessed predecessor of.
		for _, edge := range edgesOf(builder.graph, block, edges) {
			if edge.edge == Fallthrough {
				continue
			}
			successor, ok := builder.graph.Block(builder.function, edge.successor)
			if !ok {
				continue
			}
			remaining := builder.remainingPredecessors(edge.successor, len(builder.graph.Predecessors(successor)))
			remaining--
			builder.unsealedPreds[edge.successor] = remaining
			if remaining == 0 && builder.visited[edge.successor] {
				builder.fixIncompletePhis(edge.successor)
			}
		}
	}

	// The function's return value is a definition like any other and is read by nothing inside the
	// graph, so it is renamed to whatever reaches the end rather than left pointing at the original.
	builder.renameReturns()
}

// visitPlace is run's one visitor, doing what b.walking says for the block b.walkingBlock.
func (builder *ssaBuilder[G, F, B, P]) visitPlace(place *P, role Role) {
	switch builder.walking {
	case walkingUses:
		if role != Define {
			builder.useIn(builder.walkingBlock, place)
		}
	case walkingDefinitions:
		if role == Define {
			builder.defineInMaybeContext(builder.walkingBlock, place, builder.walkingContextStore)
		}
	case walkingTerminal:
		if role == Define {
			builder.defineIn(builder.walkingBlock, place)
			return
		}
		builder.useIn(builder.walkingBlock, place)
	}
}

// defineIn mints a fresh value for a definition and records it as the block's current answer.
//
// # A context binding is defined once and never redefined
//
// Upstream refuses every rename after the first for a binding it has marked as context:
//
//	// Do not redefine context references.
//	if (this.#context.has(oldId)) {
//	  return this.getPlace(oldPlace);
//	}
//
// `SSA/EnterSSA.ts:124`. That is what makes a later write reach the value a closure already
// captured. For `let x = []; useCallback(() => [x], [x]); x = makeArray();` the reassignment's
// `Mutate` lands on the identifier the callback captured, so the range widens across the memo
// marker and the rule can see that the dependency may be mutated later.
//
// Versioning that write instead mints a value at the reassignment's own order, and a mutation on a
// value whose range starts there widens nothing -- measured as `{20, 21}` against a memo block
// spanning 6 to 15, which is why the effect and the lowering were both faithful and the finding
// still did not fire.
func (builder *ssaBuilder[G, F, B, P]) defineIn(blockId BlockId, place *P) {
	builder.defineInMaybeContext(blockId, place, false)
}

// defineInMaybeContext is `defineIn`, told whether this definition is a context store.
//
// Mutates: place it is rewritten in the caller's instruction to name the definition this mints or reuses
func (builder *ssaBuilder[G, F, B, P]) defineInMaybeContext(blockId BlockId, place *P, contextStore bool) {
	binding := builder.graph.Declaration(builder.function, builder.graph.IdentifierOf(*place))
	// # A context binding is defined once and every later write reuses that definition
	//
	// Upstream's two writes to a reassigned-and-captured binding name the same identifier --
	// measured on the pinned build, `StoreContext@2 lvalueId=2 kind=Let` and
	// `StoreContext@17 lvalueId=2 kind=Reassign`. Lowering resolves both through one Babel binding
	// and nothing renumbers it afterwards.
	//
	// That shared identity is what puts the declaration and the reassignment in one disjoint class,
	// so the scope's hull spans both and covers the memo block between them. Versioning them
	// instead gives the reassignment a range starting at its own order, which widens nothing --
	// measured as `{20, 21}` against a memo block spanning 6 to 15.
	//
	// Gated on the instruction being a context store. The set is keyed by declaration, and a
	// `FunctionExpression` assigned to the same declaration would otherwise reuse the identifier
	// too, which is a real duplicate definition -- measured on
	// `let n = 1; const g = () => n; n = 2`.
	if contextStore && builder.graph.Contextual(builder.function, binding) {
		if existing, defined := builder.stateAt(blockId).defs[binding]; defined {
			*place = builder.graph.WithIdentifier(*place, existing)
			return
		}
	}
	renamed := builder.graph.Mint(builder.function, builder.graph.IdentifierOf(*place))
	builder.stateAt(blockId).defs[binding] = renamed
	*place = builder.graph.WithIdentifier(*place, renamed)
}

// useIn rewrites a use to whatever value reaches this block.
//
// Mutates: place it is rewritten in the caller's instruction to name the value that reaches this block
func (builder *ssaBuilder[G, F, B, P]) useIn(blockId BlockId, place *P) {
	*place = builder.graph.WithIdentifier(*place, builder.valueAt(*place, blockId))
}

// valueAt is Braun's lookup: which value does this binding hold on entry to this block.
//
// The order of the cases is load-bearing and is Braun's:
//
//  1. Defined here already - the local answer wins.
//  2. No predecessors - the entry block, and the binding was never defined. It is a global or a
//     capture; record it and hand back the original rather than inventing a definition.
//  3. Some predecessor unprocessed - a loop. Mint an incomplete phi. Recording it in defs before
//     recursing is what terminates the walk around the cycle.
//  4. Exactly one predecessor - no merge, so recurse and cache. A phi here would be redundant by
//     construction.
//  5. Several predecessors - a real join. Mint the result, record it, then collect operands; a
//     predecessor whose lookup comes back around to this block must find the result already bound.
func (builder *ssaBuilder[G, F, B, P]) valueAt(place P, blockId BlockId) IdentifierId {
	original := builder.graph.IdentifierOf(place)
	binding := builder.graph.Declaration(builder.function, original)
	if builder.unknown[binding] {
		return original
	}

	state := builder.stateAt(blockId)
	if renamed, defined := state.defs[binding]; defined {
		return renamed
	}

	block, ok := builder.graph.Block(builder.function, blockId)
	if !ok {
		return original
	}

	predecessors := builder.graph.Predecessors(block)
	if len(predecessors) == 0 {
		if builder.unknown == nil {
			builder.unknown = map[DeclarationId]bool{}
		}
		builder.unknown[binding] = true
		return original
	}

	remaining := builder.remainingPredecessors(blockId, len(predecessors))
	if remaining > 0 {
		renamed := builder.graph.Mint(builder.function, original)
		state.defs[binding] = renamed
		state.incompletePhis = append(state.incompletePhis, incompletePhi[P]{
			original: place,
			renamed:  builder.graph.WithIdentifier(place, renamed),
		})
		return renamed
	}

	if len(predecessors) == 1 {
		renamed := builder.valueFromPredecessor(place, predecessors[0], blockId)
		state.defs[binding] = renamed
		return renamed
	}

	renamed := builder.graph.Mint(builder.function, original)
	state.defs[binding] = renamed
	builder.addPhi(blockId, place, builder.graph.WithIdentifier(place, renamed))
	return renamed
}

// valueFromPredecessor is the value a binding holds when control arrives at blockId from predecessor.
//
// Every lookup across an edge comes through here, and today it is the predecessor's value at its end
// whatever the edge, which is right for every edge but an exceptional one. An exceptional edge leaves
// its block from the throwing instruction, before that instruction's definitions happen, so the
// handler should see the definitions from just before it. That is #2yz9ra9, live in Adamic, whose
// MayThrow emits such edges, and latent in cohere, which emits none. Its fix lands here: snapshot the
// block's definitions before its throwing instruction while walking it, and answer an Exceptional
// edge from the snapshot.
func (builder *ssaBuilder[G, F, B, P]) valueFromPredecessor(
	place P,
	predecessor BlockId,
	blockId BlockId,
) IdentifierId {
	return builder.valueAt(place, predecessor)
}

// addPhi collects one operand per predecessor and attaches the phi to the block.
//
// original is the place a lookup reads, and each operand is that place naming the predecessor's
// value, so whatever else the place carries rides along on every operand.
func (builder *ssaBuilder[G, F, B, P]) addPhi(blockId BlockId, original P, renamed P) {
	block, ok := builder.graph.Block(builder.function, blockId)
	if !ok {
		return
	}

	predecessors := builder.graph.Predecessors(block)
	operands := make(PhiOperands[P], 0, len(predecessors))
	for _, predecessorId := range predecessors {
		operands.Set(predecessorId, builder.graph.WithIdentifier(original,
			builder.valueFromPredecessor(original, predecessorId, blockId)))
	}

	builder.graph.SetPhis(block, append(builder.graph.Phis(block), &Phi[P]{Place: renamed, Operands: operands}))
}

// fixIncompletePhis fills in the operands of every phi minted before this block was sealed.
func (builder *ssaBuilder[G, F, B, P]) fixIncompletePhis(blockId BlockId) {
	state, ok := builder.existingState(blockId)
	if !ok {
		return
	}
	pending := state.incompletePhis
	state.incompletePhis = nil
	for _, phi := range pending {
		builder.addPhi(blockId, phi.original, phi.renamed)
	}
}

// renameReturns rewrites the function's Returns place to the value that reaches the exit.
//
// Every `return` stores into one identifier, so after renaming the stores there are several values
// and the Returns place still names the original. It is resolved against the blocks that actually
// end in a Return, which is where the value is observable. An IR with no Returns place skips this.
func (builder *ssaBuilder[G, F, B, P]) renameReturns() {
	returns := builder.graph.Returns(builder.function)
	if returns == nil {
		return
	}
	binding := builder.graph.Declaration(builder.function, builder.graph.IdentifierOf(*returns))
	if builder.unknown[binding] {
		return
	}
	for _, block := range builder.graph.Blocks(builder.function) {
		if !builder.graph.EndsInReturn(block) {
			continue
		}
		if state, ok := builder.existingState(builder.graph.Id(block)); ok {
			if renamed, defined := state.defs[binding]; defined {
				*returns = builder.graph.WithIdentifier(*returns, renamed)
				return
			}
		}
	}
}
