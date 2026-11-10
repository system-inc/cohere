package intermediate_representation

// Lifting's working memory (#npb86tz): the tables a round makes, kept for the next round and, through a pool, for the
// next function, so a run grows them a handful of times rather than making them afresh every round.

import (
	"sync"
)

// liftingScratchPool holds the scratch of functions done lifting, for the next function any goroutine lifts.
var liftingScratchPool = sync.Pool{New: func() any { return &liftingScratch{} }}

// liftingScratchLocalsLimit is the most Locals a pooled localsInBlocks may have held. Clearing a map costs its
// capacity, not its length, so one that has held more is dropped, not pooled, and a function with a handful of Locals
// never clears a table sized for another's thousands.
const liftingScratchLocalsLimit = 256

// liftingScratch is lifting's working memory, kept across a function's rounds and, through liftingScratchPool,
// across the functions lifted one after another, so a round reuses the arrays the last one grew rather than making
// its own (#npb86tz): every function that lifts anything runs at least two rounds, the last lifting nothing, and
// each round made all of these afresh. Only capacity outlives a round: each round clears what it reads before
// reading it, and release clears every pointer before the pool takes the scratch back, so a pooled one holds no
// function's representation alive.
type liftingScratch struct {
	// work, heads and runDefers are the round's per-block tables, by block index.
	work      []liftingWork
	heads     []int
	runDefers []int

	liftable liftingLiftableScratch

	// allocations are the Allocations the round lifts.
	allocations []*Allocation

	// localsInBlocks says which of the function's Locals the round's walk finds in a block: a map of Locals alone,
	// most functions holding a handful, where go/ir marks every Allocation it walks. It is nil until a function
	// with Locals lifts, and empty when the function has none. localsHeld is the most it has held since the pool
	// last took the scratch back, which release reads: clearing empties a map's length and keeps its capacity.
	localsInBlocks map[*Allocation]bool
	localsHeld     int

	// lifting is the round's function as the core sees it. Its blocks point into blockValues, each block's slots are
	// carved from slots on its first place or deletion, its placed views from placed, and its order is order.
	lifting     liftingFunction
	blockValues []liftingBlock
	slots       []liftingSlot
	placed      []int32
	visited     []bool
	order       []*liftingBlock

	// definitions are the resolve's table, by identifier.
	definitions []liftingDefinition
}

// release clears every pointer scratch holds, to the full capacity of each array, since a round that used less
// than an earlier one leaves the earlier one's tail, and drops a localsInBlocks that has held more than
// liftingScratchLocalsLimit.
func (scratch *liftingScratch) release() {
	clear(scratch.work[:cap(scratch.work)])
	clear(scratch.liftable.pending[:cap(scratch.liftable.pending)])
	clear(scratch.allocations[:cap(scratch.allocations)])
	if scratch.localsHeld > liftingScratchLocalsLimit {
		scratch.localsInBlocks = nil
	} else {
		clear(scratch.localsInBlocks)
	}
	scratch.localsHeld = 0
	clear(scratch.lifting.blocks[:cap(scratch.lifting.blocks)])
	scratch.lifting = liftingFunction{
		blocks:       scratch.lifting.blocks[:0],
		declarations: scratch.lifting.declarations[:0],
	}
	clear(scratch.blockValues[:cap(scratch.blockValues)])
	clear(scratch.order[:cap(scratch.order)])
	clear(scratch.definitions[:cap(scratch.definitions)])
}
