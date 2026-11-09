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
// Dominance is still what correctness is stated against - see `VerifyStaticSingleAssignment` - but it is computed
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
// when the last predecessor lands. Recording the definition before recursing is also what stops the
// lookup recursing forever around the cycle.
//
// # A throw's edge, which carries less than the block it leaves
//
// A block with an Exceptional edge out ends in an instruction that may throw, and its edge to the
// handler leaves before that instruction's definitions happen. In `local = f()` where f throws and the
// handler reads local, the handler reads the old value. So a lookup through that edge reads what each
// binding held just before the last instruction defined it (valueFromPredecessor), never the block's
// final answer, which is what every other edge carries. It is Adamic's construction, adopted from its
// internal/flow (#2yz9ra9, #js7tdfg), where it is live; cohere's TypeScript IR marks React's MaybeThrow
// handler Exceptional and so takes it too.
//
// A block both of whose edges reach one successor, a Real and an Exceptional edge to the same block, is
// one predecessor of it with one phi operand, which can't carry both values; that edge carries the
// block's final answer, as before. Neither IR emits one (#mv39hpt).
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
// a bug in its adapter or its lowering, and the panic names it. VerifyStaticSingleAssignment reports the same thing
// without constructing.
func Construct[G Graph[F, B, P], F any, B comparable, P any](graph G, function F) {
	if entry, predecessors, entered := entryPredecessors(graph, function); entered {
		// Invariant: Graph.Entry names a block no edge enters, and neither IR builds a function whose entry has a
		// predecessor; one that did has a bug in its adapter or its lowering, which this message names.
		panic(fmt.Sprintf("static_single_assignment.Construct: the entry block bb%d has predecessors %v; "+
			"Graph.Entry must be a block no edge enters", entry, predecessors))
	}

	// Phis from a previous run are dropped, because this pass appends them and cannot reconcile
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
		graph:    graph,
		function: function,
	}
	builder.run()

	// Elimination is part of construction rather than an optional follow-up, because Braun's
	// placement cannot avoid producing redundant phis and their share is not marginal. Measured over
	// 1,945 functions of real TypeScript: 24,418 phis before elimination, 2,746 after. Leaving them
	// in would mean 89% of the merge points a consumer sees are not merges, and every pass above
	// this would have to re-derive which ones are real.
	//
	// Both numbers moved when captures landed (from 21,654 and 2,770), in opposite directions, and
	// the split is worth knowing. Captures add raw phis, because a captured binding is now a real
	// value that gets merged at every join it crosses rather than an opaque name. They remove
	// surviving ones, because a variable only reassigned from inside a closure now takes that write
	// as a `StoreContext` in the nested function instead of leaving the outer versions to be merged.
	EliminateRedundantPhis(graph, function)
}

// ssaState is one block's view: what each original binding currently resolves to, and the phis
// whose operands are still waiting on an unprocessed predecessor.
type ssaState[P any] struct {
	// defs maps a binding to the value it holds on entry to, or within, this block.
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
	// bound in defs, or in entry; only the operands are outstanding.
	incompletePhis []incompletePhi[P]

	// handlers are the blocks this block reaches by an Exceptional edge alone, no Real edge beside it:
	// the edges that leave before its last instruction's definitions.
	handlers []BlockId

	// beforeThrow is, for a block with handlers, what each binding its last instruction defines held
	// just before that definition: what an edge to a handler carries.
	beforeThrow map[DeclarationId]beforeDefinition

	// entry is what a binding held on entry to this block, for a binding a handler's edge needs from
	// before the block defined it. defs can't hold it: by the end of the block, defs holds the block's own
	// definition. Made on its first need, since few blocks throw.
	entry map[DeclarationId]IdentifierId
}

// beforeDefinition is what a binding held just before an instruction defined it: a value the block had
// already given it, or, when onEntry, whatever it held on entry to the block, looked up only if a handler
// reads it, since a lookup when it is recorded would walk off the entry for a binding the instruction
// declares and mark it unknown for the whole function.
type beforeDefinition struct {
	value   IdentifierId
	onEntry bool
}

// incompletePhi is a phi awaiting operands.
type incompletePhi[P any] struct {
	// original is the pre-SSA identifier being merged, the key a lookup uses.
	original P
	// renamed is the phi's result, already minted and already visible in defs.
	renamed P
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
	// walkingBeforeThrow records, for each definition of a throwing instruction, what its binding holds
	// before it, and renames nothing.
	walkingBeforeThrow
)

func newSsaState[P any]() *ssaState[P] {
	return &ssaState[P]{defs: map[DeclarationId]IdentifierId{}}
}
