package intermediate_representation

// Lifting's working memory (#npb86tz): the tables a round makes, kept for the next round and, through a pool, for the
// next function, so a run grows them a handful of times rather than making them afresh every round.

import (
	"sync"

	"github.com/system-inc/cohere/static_single_assignment"
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

	// allocations are the Allocations the round lifts. allocationsUsed is the most any earlier round has lifted since
	// the pool last took the scratch back, taken as each round starts the array again, which release reads along
	// with the last round's own length.
	allocations     []*Allocation
	allocationsUsed int

	// localsInBlocks says which of the function's Locals the round's walk finds in a block: a map of Locals alone,
	// most functions holding a handful, where go/ir marks every Allocation it walks. It is nil until a function
	// with Locals lifts, and empty when the function has none. localsHeld is the most it has held since the pool
	// last took the scratch back, which release reads: clearing empties a map's length and keeps its capacity.
	localsInBlocks map[*Allocation]bool
	localsHeld     int

	// lifting is the round's function as the core sees it. Its blocks point into blockValues, each block's slots are
	// carved from slots on its first place or deletion, its placed views from placed, and its order is order.
	// blockValuesUsed is the most blocks a round has prepared since the pool last took the scratch back, which
	// release reads: a round of fewer blocks leaves an earlier round's tail in blockValues, in order and in lifting's
	// blocks, which the prepare sizes alongside blockValues.
	lifting         liftingFunction
	blockValues     []liftingBlock
	blockValuesUsed int
	slots           []liftingSlot
	placed          []int32
	visited         []bool
	order           []*liftingBlock

	// predecessors are every block's predecessors by id, end to end, each block's view carved from them. They hold
	// no pointer, so release leaves them, and each round starts them again at zero length.
	predecessors []static_single_assignment.BlockId

	// definitions are the resolve's table, by identifier. definitionsUsed is the longest a resolve has made it since
	// the pool last took the scratch back, which release reads.
	definitions     []liftingDefinition
	definitionsUsed int
}

// release clears every pointer scratch holds, and drops a localsInBlocks that has held more than
// liftingScratchLocalsLimit.
//
// allocations, lifting's blocks, blockValues, order and definitions are cleared only as far as the function used
// them (#w5dbvdm): cleared to its capacity, each cost every release as much as the largest function the array ever
// held, and on a host run over cohere's own root allocations cleared 583 million slots where the functions had
// used 0.58 million, and definitions 1,452 million for 2.12 million. A pooled array is all zero past what the
// function used, because it was zero when made, every round writes only below its own length, and each release
// clears the longest length since the one before. An array a round outgrows is replaced by a new zeroed one and
// the old one is dropped, so nothing stale is kept there either.
//
// work and pending are still cleared to the full capacity of each array, since a round that used less than an
// earlier one leaves the earlier one's tail. On that run they cleared 21 million and 6.9 million slots, too few to
// be worth a mark each, and each is sized on a path of its own: work by the round, pending by liftable's stack as
// it grows.
func (scratch *liftingScratch) release() {
	clear(scratch.work[:cap(scratch.work)])
	clear(scratch.liftable.pending[:cap(scratch.liftable.pending)])
	// The last round's Allocations are still at their length, which no later round has taken into allocationsUsed.
	clear(scratch.allocations[:max(scratch.allocationsUsed, len(scratch.allocations))])
	scratch.allocationsUsed = 0
	if scratch.localsHeld > liftingScratchLocalsLimit {
		scratch.localsInBlocks = nil
	} else {
		clear(scratch.localsInBlocks)
	}
	scratch.localsHeld = 0
	clear(scratch.lifting.blocks[:scratch.blockValuesUsed])
	scratch.lifting = liftingFunction{
		blocks:       scratch.lifting.blocks[:0],
		declarations: scratch.lifting.declarations[:0],
	}
	clear(scratch.blockValues[:scratch.blockValuesUsed])
	// A round appends each block it prepared to order once, so blockValuesUsed bounds what order holds too. order
	// grows by append, apart from blockValues, so a walk that stopped early leaves its capacity the smaller.
	clear(scratch.order[:min(scratch.blockValuesUsed, cap(scratch.order))])
	scratch.blockValuesUsed = 0
	clear(scratch.definitions[:scratch.definitionsUsed])
	scratch.definitionsUsed = 0
}
