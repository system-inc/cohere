// Single static assignment form: rename every definition so each value is written exactly once,
// and insert phi nodes where control from several predecessors rejoins.
//
// This is Braun et al., "Simple and Efficient Construction of Static Single Assignment Form"
// (CC 2013), in the sealed-block form upstream uses. React's own specification of the pass is at
// `compiler/packages/babel-plugin-react-compiler/docs/passes/02-enterSSA.md`, and the shape here
// follows it closely enough that the two can be read side by side.
//
// # Why Braun rather than Cytron
//
// The textbook construction computes the iterated dominance frontier, places empty phis at every
// block in it, then renames in a second walk over the dominator tree. Braun places a phi only when
// a lookup actually crosses a join with disagreeing predecessors, which means it never places one
// that redundancy elimination would immediately remove, and it needs no dominance frontier at all.
//
// Dominance is still what CORRECTNESS is stated against - see `VerifySSA` - but it is computed
// there, over this graph, for checking rather than for construction.
//
// # The one property everything rests on
//
// A block may be processed only after every predecessor that is not a back edge, so that a lookup
// into a predecessor finds a finished answer. The block slice is in reverse postorder, which is
// exactly that guarantee, and an IR's Finalize establishes it. A caller that restructured the graph
// without re-running it gets silent nonsense, which is why an IR's Construct re-runs it first.
//
// # Loops, which is where a construction that looks right on straight-line code breaks
//
// A loop header is reached from before the loop and from the back edge, and the back edge's block
// has not been processed when the header is. Braun's answer is the incomplete phi: when a lookup
// reaches a block with unprocessed predecessors, mint the phi's result immediately, record it as
// the block's definition so the loop body's reads bind to it, and leave the operands to be filled
// when the last predecessor lands. Recording the definition BEFORE recursing is also what stops the
// lookup recursing forever around the cycle.
//
// # What is deliberately not here
//
// Redundant-phi elimination is a separate pass, `EliminateRedundantPhis`, in eliminate.go, which
// Construct runs. Recursion into nested functions is each IR's own, around this.
package static_single_assignment

import "fmt"

// Construct converts one function to single static assignment form, in place, then eliminates the
// redundant phis that placement produces. It does not re-establish the graph's invariants or recurse
// into nested functions; an IR's own Construct does both around it.
//
// After it returns: every identifier that a source binding takes is written exactly once, every use
// names the definition that actually reaches it, and each block's phis hold a phi wherever a
// binding's value depends on which predecessor control arrived from.
//
// It refuses, by panicking with the reason, a function whose entry block some edge enters, which
// Graph.Entry rules out. Its lookup walks back through predecessors until it finds a definition or a
// block with none, and an entry on a cycle of blocks with one predecessor each gives it neither: before
// this check, that was a stack overflow in valueAt. Neither IR builds such a function; one that did has
// a bug in its adapter or its lowering, and the panic names it. VerifySSA reports the same thing
// without constructing.
func Construct[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) {
	if entry, predecessors, entered := entryPredecessors(graph, function); entered {
		panic(fmt.Sprintf("static_single_assignment.Construct: the entry block bb%d has predecessors %v; "+
			"Graph.Entry must be a block no edge enters", entry, predecessors))
	}

	// Phis from a previous run are dropped, because this pass APPENDS them and cannot reconcile
	// what it did not mint.
	//
	// A caller that restructures the graph and runs construction again is the case: inlining an
	// immediately invoked function expression does exactly that. The phis left behind name
	// identifiers from before the renumbering, so they define a value nothing produces, join no
	// class in the disjoint partition, and take no scope. Measured over the corpus, a second run
	// takes phis 1,548 to 3,096 -- every one duplicated -- and phis naming an undefined operand
	// 221 to 1,730.
	//
	// Same intent as recomputing reverse postorder first: re-establish the invariant rather than
	// trust it. Braun's algorithm derives every phi it needs from the graph, so nothing is lost by
	// discarding the previous answer, and `EliminateRedundantPhis` below then sees only phis this
	// run placed.
	var noBlock B
	for _, block := range graph.Blocks(function) {
		if block != noBlock {
			graph.SetPhis(block, nil)
		}
	}

	builder := &ssaBuilder[G, F, B, P]{
		graph:         graph,
		function:      function,
		states:        map[BlockId]*ssaState[P]{},
		unsealedPreds: map[BlockId]int{},
		unknown:       map[DeclarationId]bool{},
	}
	builder.run()

	// Elimination is part of construction rather than an optional follow-up, because Braun's
	// placement cannot avoid producing redundant phis and their share is not marginal. Measured over
	// 1,945 functions of real TypeScript: 24,418 phis before elimination, 2,746 after. Leaving them
	// in would mean 89% of the merge points a consumer sees are not merges, and every pass above
	// this would have to re-derive which ones are real.
	//
	// Both numbers moved when captures landed (from 21,654 and 2,770), in opposite directions, and
	// the split is worth knowing. Captures ADD raw phis, because a captured binding is now a real
	// value that gets merged at every join it crosses rather than an opaque name. They REMOVE
	// surviving ones, because a variable only reassigned from inside a closure now takes that write
	// as a `StoreContext` in the nested function instead of leaving the outer versions to be merged.
	EliminateRedundantPhis(graph, function)
}

// ssaState is one block's view: what each original binding currently resolves to, and the phis
// whose operands are still waiting on an unprocessed predecessor.
type ssaState[P any] struct {
	// defs maps a BINDING to the value it holds on entry to, or within, this block.
	//
	// The key is the DeclarationId rather than the IdentifierId, and that is the single most
	// important decision in this file. Lowering already mints a fresh IdentifierId for every store
	// to a variable, so `let y` reassigned twice arrives as three distinct identifiers sharing one
	// declaration. Keying on the identifier would make each store a different variable, and a
	// lookup would never find a predecessor's definition because the predecessor stored under a
	// different key. Keying on the declaration is what makes them one variable with several values,
	// which is the fact SSA exists to express.
	defs map[DeclarationId]IdentifierId

	// incompletePhis are phis minted before every predecessor was processed. Their result is already
	// bound in defs; only the operands are outstanding.
	incompletePhis []incompletePhi[P]
}

// incompletePhi is a phi awaiting operands.
type incompletePhi[P any] struct {
	// original is the pre-SSA identifier being merged, the key a lookup uses.
	original P
	// renamed is the phi's result, already minted and already visible in defs.
	renamed P
}

type ssaBuilder[G Graph[F, B, P], F any, B comparable, P any] struct {
	graph    G
	function F

	states map[BlockId]*ssaState[P]

	// unsealedPreds counts, per block, how many predecessors have not yet been processed. A block
	// whose count reaches zero is sealed and its incomplete phis can be filled.
	//
	// Absent from the map means "not yet decremented", which is not the same as zero; the read sites
	// initialise from the predecessor count on first touch for exactly that reason.
	unsealedPreds map[BlockId]int

	// unknown holds bindings a lookup walked off the entry block without finding. In cohere they are
	// globals, or captures from an enclosing function; in Adamic, which keeps both out of its graph, a
	// variable read before anything defines it. They are left un-renamed: there is no definition in
	// this function to merge, so a phi over them would be an invention.
	unknown map[DeclarationId]bool

	// visited records blocks already processed, so sealing only fills phis for a block whose body
	// has actually been walked.
	visited map[BlockId]bool

	// walking, walkingBlock and walkingContextStore are where run's walk is, read by visitPlace.
	walking             walking
	walkingBlock        BlockId
	walkingContextStore bool
}

func (builder *ssaBuilder[G, F, B, P]) run() {
	builder.visited = map[BlockId]bool{}

	// Parameters are definitions in the entry block. Renaming them is what makes a reassigned
	// parameter behave like any other binding rather than like a global.
	entryId := builder.graph.Entry(builder.function)
	if _, ok := builder.graph.Block(builder.function, entryId); !ok {
		return
	}
	builder.states[entryId] = newSSAState[P]()
	params := builder.graph.Params(builder.function)
	for index := range params {
		builder.defineIn(entryId, &params[index])
	}

	// One visitor for every place, made once with the builder, which reads what to do from
	// b.walking. A visitor handed to a Graph method escapes, since the compiler cannot see through a
	// type parameter's method, and so does every local it captures; one per instruction would be an
	// allocation per instruction.
	visit := func(place *P, role Role) { builder.visitPlace(place, role) }
	edges := edgeScratchPool.Get().(*edgeScratch)
	defer edgeScratchPool.Put(edges)

	for _, block := range builder.graph.Blocks(builder.function) {
		blockId := builder.graph.Id(block)
		builder.walkingBlock = blockId
		if _, exists := builder.states[blockId]; !exists {
			builder.states[blockId] = newSSAState[P]()
		}
		builder.visited[blockId] = true

		count := builder.graph.InstructionCount(builder.function, block)
		for index := 0; index < count; index++ {
			// Uses first, then definitions. `x = x + 1` must read the OLD x before the store mints
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
			remaining, seen := builder.unsealedPreds[edge.successor]
			if !seen {
				remaining = len(builder.graph.Predecessors(successor))
			}
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

// walking is which of run's three visits a place is in.
type walking uint8

const (
	// walkingUses renames an instruction's uses, and passes over its definitions.
	walkingUses walking = iota
	// walkingDefinitions renames an instruction's definitions, and passes over its uses.
	walkingDefinitions
	// walkingTerminal renames a terminal's places, uses and definitions both.
	walkingTerminal
)

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

func newSSAState[P any]() *ssaState[P] {
	return &ssaState[P]{defs: map[DeclarationId]IdentifierId{}}
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
func (builder *ssaBuilder[G, F, B, P]) defineInMaybeContext(blockId BlockId, place *P, contextStore bool) {
	binding := builder.graph.Declaration(builder.function, builder.graph.IdentifierOf(*place))
	// # A context binding is defined once and every later write reuses that definition
	//
	// Upstream's two writes to a reassigned-and-captured binding name the SAME identifier --
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
		if existing, defined := builder.states[blockId].defs[binding]; defined {
			*place = builder.graph.WithIdentifier(*place, existing)
			return
		}
	}
	renamed := builder.graph.Mint(builder.function, builder.graph.IdentifierOf(*place))
	builder.states[blockId].defs[binding] = renamed
	*place = builder.graph.WithIdentifier(*place, renamed)
}

// useIn rewrites a use to whatever value reaches this block.
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
//  3. Some predecessor unprocessed - a loop. Mint an incomplete phi. Recording it in defs BEFORE
//     recursing is what terminates the walk around the cycle.
//  4. Exactly one predecessor - no merge, so recurse and cache. A phi here would be redundant by
//     construction.
//  5. Several predecessors - a real join. Mint the result, record it, THEN collect operands; a
//     predecessor whose lookup comes back around to this block must find the result already bound.
func (builder *ssaBuilder[G, F, B, P]) valueAt(place P, blockId BlockId) IdentifierId {
	original := builder.graph.IdentifierOf(place)
	binding := builder.graph.Declaration(builder.function, original)
	if builder.unknown[binding] {
		return original
	}

	state, ok := builder.states[blockId]
	if !ok {
		state = newSSAState[P]()
		builder.states[blockId] = state
	}
	if renamed, defined := state.defs[binding]; defined {
		return renamed
	}

	block, ok := builder.graph.Block(builder.function, blockId)
	if !ok {
		return original
	}

	predecessors := builder.graph.Predecessors(block)
	if len(predecessors) == 0 {
		builder.unknown[binding] = true
		return original
	}

	remaining, seen := builder.unsealedPreds[blockId]
	if !seen {
		remaining = len(predecessors)
	}
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
func (builder *ssaBuilder[G, F, B, P]) valueFromPredecessor(place P, predecessor BlockId, blockId BlockId) IdentifierId {
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
	state, ok := builder.states[blockId]
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
		if state, ok := builder.states[builder.graph.Id(block)]; ok {
			if renamed, defined := state.defs[binding]; defined {
				*returns = builder.graph.WithIdentifier(*returns, renamed)
				return
			}
		}
	}
}
