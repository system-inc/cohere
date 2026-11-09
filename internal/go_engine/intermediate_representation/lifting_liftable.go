// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file is go/ir's liftable (lift.go:599-860 at honnef.co/go/tools v0.8.1), ported to our names: which
// Allocations lifting may turn into values, and, for one whose address escapes partway through the function, the
// instructions that split it into a liftable cell and an escaping one. Lifting itself is lifting.go, through the
// shared core rather than go/ir's dominance frontiers (#ee33njz).

import (
	"slices"
)

// liftingWork is what liftable decides to change in one block before the core lifts: instructions to insert
// before an existing one, and Allocations to rename from an instruction on.
type liftingWork struct {
	insertInstructions map[Instruction][]Instruction
	renameAllocations  []liftingRename
}

// liftingRename moves every use of from, in a block from startingAt on, to to.
type liftingRename struct {
	from       *Allocation
	to         *Allocation
	startingAt int
}

// liftingLiftableScratch is liftable's working memory, kept across one round's Allocations so each reuses the
// last one's slices rather than making its own (#beapx0q): go/ir reuses its liftableBlockMap the same way.
type liftingLiftableScratch struct {
	descriptions        []liftingBlockDescription
	seen                []bool
	predecessorHasStore []bool

	// pending are the blocks still to mark unliftable, a stack in place of go/ir's recursion.
	pending []*BasicBlock
}

// liftingCleared is buffer at length, every element zero, reusing its array when it is long enough.
func liftingCleared[Element any](buffer []Element, length int) []Element {
	if cap(buffer) < length {
		return make([]Element, length)
	}
	cleared := buffer[:length]
	clear(cleared)
	return cleared
}

// liftingBlockDescription is what liftable learns of one block for one Allocation.
type liftingBlockDescription struct {
	// isUnliftable is whether the block is (partially) unliftable, because it holds an unliftable use or an
	// unliftable block reaches it.
	isUnliftable     bool
	hasLiftableLoad  bool
	hasLiftableOther bool

	// storeInPredecessors is whether the stores that split the Allocation go at the end of the block's
	// predecessors, because the unliftable use is a phi.
	storeInPredecessors bool

	lastLiftable    int
	firstUnliftable int
}

// liftingLiftable reports whether allocation can be lifted, recording in work the instructions that split one
// that is only partially liftable. Numbers the function's instructions per block first; heads holds each block's
// first non-phi index.
//
// In the trivial case every use of the Allocation is a Load from it or a Store to it, and nothing is recorded.
// Otherwise its uses fall on two sides of a boundary in the control flow: once an unliftable use (fn(&x), say) is
// reached, the Allocation is unliftable for the rest of that block and in every block reachable from it. The
// recorded instructions split it there: a new Allocation takes the unliftable uses, and a Load and Store copy the
// lifted value into it at each crossing, so the lifting machinery lifts the old one whole and leaves the new one
// in memory. In Go:
//
//	func foo() {             func fooSplitAlloc() {
//		x := 32                  x := 32
//		if cond {                var x_ int
//			println(x)           if cond {
//			escape(&x)               println(x)
//			println(x)               x_ = x
//		} else {                     escape(&x_)
//			println(x)               println(x_)
//		}                        } else {
//		println(x)                   println(x)
//	}                                x_ = x
//	                             }
//	                             println(x_)
//	                         }
//
// Mutates: scratch its slices are reused across the round's Allocations, as go/ir reuses its liftableBlockMap
func liftingLiftable(allocation *Allocation, work []liftingWork, heads []int, scratch *liftingLiftableScratch) bool {
	function := allocation.block.parent

	// Don't lift result values in functions that defer calls that may recover from panic: the Recover block
	// reads them from memory.
	if function.Recover != nil && slices.Contains(function.results, allocation) {
		return false
	}

	scratch.descriptions = liftingCleared(scratch.descriptions, len(function.Blocks))
	descriptions := scratch.descriptions
	for _, block := range function.Blocks {
		descriptions[block.Index].lastLiftable = -1
		descriptions[block.Index].firstUnliftable = len(block.Instructions) + 1
	}

	// Look at all uses of the Allocation and deduce which blocks have liftable or unliftable instructions.
	for _, referrer := range allocation.referrers {
		// Find the first unliftable use.
		description := &descriptions[referrer.Block().Index]
		hasUnliftable := false
		inHead := false
		switch referrer := referrer.(type) {
		case *Store:
			if referrer.Value == allocation {
				hasUnliftable = true
			}
		case *Load:
		case *debugReference:
		case *Phi:
			inHead = true
			hasUnliftable = true
		default:
			hasUnliftable = true
		}
		if hasUnliftable {
			description.isUnliftable = true
			if int(referrer.Id()) < description.firstUnliftable {
				description.firstUnliftable = int(referrer.Id())
			}
			if inHead {
				description.storeInPredecessors = true
				description.firstUnliftable = 0
			}
		}
	}

	for _, referrer := range allocation.referrers {
		// Find the last liftable use, taking the first unliftable one found above into account.
		description := &descriptions[referrer.Block().Index]
		if int(referrer.Id()) >= description.firstUnliftable {
			continue
		}
		hasLiftable := false
		switch referrer := referrer.(type) {
		case *Store:
			if referrer.Value != allocation {
				description.hasLiftableOther = true
				hasLiftable = true
			}
		case *Load:
			description.hasLiftableLoad = true
			hasLiftable = true
		case *debugReference:
			description.hasLiftableOther = true
		}
		if hasLiftable && int(referrer.Id()) > description.lastLiftable {
			description.lastLiftable = int(referrer.Id())
		}
	}

	for index := range descriptions {
		// The first unliftable use moves to one after the last liftable one.
		if descriptions[index].lastLiftable == -1 && !descriptions[index].storeInPredecessors {
			// No liftable instruction for this Allocation in this block: the boundary is the first non-phi
			// instruction, so no store lands before a phi.
			descriptions[index].firstUnliftable = heads[index]
		} else {
			descriptions[index].firstUnliftable = descriptions[index].lastLiftable + 1
		}
	}

	// A block a (partially) unliftable block reaches is unliftable whole, and its stores go in its
	// predecessors.
	scratch.seen = liftingCleared(scratch.seen, len(function.Blocks))
	seen := scratch.seen
	pending := scratch.pending[:0]
	for _, block := range function.Blocks {
		if !descriptions[block.Index].isUnliftable {
			continue
		}
		pending = append(pending, block.Successors...)
		for len(pending) > 0 {
			reached := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if seen[reached.Index] {
				continue
			}
			seen[reached.Index] = true
			description := &descriptions[reached.Index]
			description.hasLiftableLoad = false
			description.hasLiftableOther = false
			description.isUnliftable = true
			description.firstUnliftable = 0
			description.storeInPredecessors = true
			pending = append(pending, reached.Successors...)
		}
	}
	scratch.pending = pending

	hasLiftableLoad := false
	hasLiftableOther := false
	hasUnliftable := false
	for _, block := range function.Blocks {
		description := &descriptions[block.Index]
		hasLiftableLoad = hasLiftableLoad || description.hasLiftableLoad
		hasLiftableOther = hasLiftableOther || description.hasLiftableOther
		if description.isUnliftable {
			hasUnliftable = true
		}
	}
	switch {
	case !hasLiftableLoad && !hasLiftableOther:
		// There are no liftable uses.
		return false
	case !hasUnliftable:
		// The Allocation is entirely liftable without splitting.
		return true
	case !hasLiftableLoad:
		// Only stores are liftable. Lifting them would loop forever when lifting to a fixed point, since the
		// new Allocations are stored into again and again and those stores are their only liftable uses.
		return false
	}

	split := &Allocation{Heap: true}
	split.setBlock(allocation.block)
	split.setType(allocation.typ)
	split.setSource(allocation.source)
	split.comment = "split alloc"
	work[allocation.block.Index].insertInstructions[allocation] = append(
		work[allocation.block.Index].insertInstructions[allocation], split)

	scratch.predecessorHasStore = liftingCleared(scratch.predecessorHasStore, len(function.Blocks))
	predecessorHasStore := scratch.predecessorHasStore
	for _, block := range function.Blocks {
		description := &descriptions[block.Index]
		if !description.isUnliftable {
			continue
		}
		work[block.Index].renameAllocations = append(work[block.Index].renameAllocations,
			liftingRename{from: allocation, to: split, startingAt: description.firstUnliftable})

		propagate := func(in *BasicBlock, before Instruction) {
			load := &Load{X: allocation}
			store := &Store{Address: split, Value: load}
			load.setType(deref(allocation.typ))
			load.setBlock(in)
			load.comment = "split alloc"
			store.setBlock(in)
			store.comment = "split alloc"
			liftingAddToOperandsReferrers(load)
			liftingAddToOperandsReferrers(store)
			work[in.Index].insertInstructions[before] = append(work[in.Index].insertInstructions[before], load,
				store)
		}

		if !description.storeInPredecessors {
			// The store goes right before the first unliftable use.
			propagate(block, block.Instructions[description.firstUnliftable])
			continue
		}
		for _, predecessor := range block.Predecessors {
			if descriptions[predecessor.Index].isUnliftable {
				continue
			}
			// A predecessor the Allocation doesn't dominate can never see it, so it stores nothing; the
			// edges that matter are the ones it dominates.
			if !allocation.block.Dominates(predecessor) {
				continue
			}
			// One propagation per predecessor: a redundant one is liftable, and lifting to a fixed point
			// would then never end.
			if predecessorHasStore[predecessor.Index] {
				continue
			}
			predecessorHasStore[predecessor.Index] = true
			propagate(predecessor, predecessor.Instructions[len(predecessor.Instructions)-1])
		}
	}
	return true
}
