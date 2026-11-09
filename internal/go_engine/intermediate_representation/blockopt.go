// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// Simple block optimizations to simplify the control flow graph.

// TODO(adonovan): opt: instead of creating several "unreachable" blocks
// per function in the Builder, reuse a single one (e.g. at Blocks[1])
// to reduce garbage.

import (
	"fmt"
	"os"
)

// If true, show progress at each successive iteration of
// optimizeBlocks.  Very verbose.
const debugBlockOpt = false

// markReachable sets Index=-1 for all blocks reachable from `block`.
func markReachable(block *BasicBlock) {
	block.gaps = -1
	for _, succ := range block.Successors {
		if succ.gaps == 0 {
			markReachable(succ)
		}
	}
}

// deleteUnreachableBlocks marks all reachable blocks of `function` and
// eliminates (nils) all others, including possibly cyclic subgraphs.
func deleteUnreachableBlocks(function *Function) {
	const white, black = 0, -1
	// We borrow b.gaps temporarily as the mark bit.
	for _, block := range function.Blocks {
		block.gaps = white
	}
	markReachable(function.Blocks[0])
	if function.Recover != nil {
		markReachable(function.Recover)
	}
	for index, block := range function.Blocks {
		if block.gaps == white {
			for _, successor := range block.Successors {
				if successor.gaps == black {
					successor.removePredecessor(block) // delete white->black edge
				}
			}
			if debugBlockOpt {
				fmt.Fprintln(os.Stderr, "unreachable", block)
			}
			function.Blocks[index] = nil // delete b
		}
	}
	function.removeNilBlocks()
}

// jumpThreading attempts to apply simple jump-threading to block `block`,
// in which a->b->c become a->c if `block` is just a Jump.
// The result is true if the optimization was applied.
func jumpThreading(function *Function, block *BasicBlock) bool {
	if block.Index == 0 {
		return false // don't apply to entry block
	}
	if len(block.Instructions) == 0 {
		return false
	}
	for _, pred := range block.Predecessors {
		switch pred.Control().(type) {
		case *ConstantSwitch:
			// don't optimize away the head blocks of switch statements
			return false
		}
	}
	if _, ok := block.Instructions[0].(*Jump); !ok {
		return false // not just a jump
	}
	successor := block.Successors[0]
	if successor == block {
		return false // don't apply to degenerate jump-to-self.
	}
	if successor.hasPhi() {
		return false // not sound without more effort
	}
	for index, predecessor := range block.Predecessors {
		predecessor.replaceSuccessor(block, successor)

		// If a now has two edges to c, replace its degenerate If by Jump.
		if len(predecessor.Successors) == 2 &&
			predecessor.Successors[0] == successor &&
			predecessor.Successors[1] == successor {
			jump := new(Jump)
			jump.setBlock(predecessor)
			predecessor.Instructions[len(predecessor.Instructions)-1] = jump
			predecessor.Successors = predecessor.Successors[:1]
			successor.removePredecessor(block)
		} else {
			if index == 0 {
				successor.replacePredecessor(block, predecessor)
			} else {
				successor.Predecessors = append(successor.Predecessors, predecessor)
			}
		}

		if debugBlockOpt {
			fmt.Fprintln(os.Stderr, "jumpThreading", predecessor, block, successor)
		}
	}
	function.Blocks[block.Index] = nil // delete b
	return true
}

// fuseBlocks attempts to apply the block fusion optimization to block
// a, in which a->b becomes ab if len(a.Successors)==len(b.Predecessors)==1.
// The result is true if the optimization was applied.
func fuseBlocks(function *Function, a *BasicBlock) bool {
	if len(a.Successors) != 1 {
		return false
	}
	b := a.Successors[0]
	if len(b.Predecessors) != 1 {
		return false
	}
	if _, ok := a.Instructions[len(a.Instructions)-1].(*Panic); ok {
		// panics aren't simple jumps, they have side effects.
		return false
	}

	// Degenerate &&/|| ops may result in a straight-line CFG
	// containing φ-nodes. (Ideally we'd replace such them with
	// their sole operand but that requires Referrers, built later.)
	if b.hasPhi() {
		return false // not sound without further effort
	}

	// Eliminate jump at end of A, then copy all of B across.
	a.Instructions = append(a.Instructions[:len(a.Instructions)-1], b.Instructions...)
	for _, instr := range b.Instructions {
		instr.setBlock(a)
	}

	// A inherits B's successors
	a.Successors = append(a.successorsSpace[:0], b.Successors...)

	// Fix up Predecessors links of all successors of B.
	for _, c := range b.Successors {
		c.replacePredecessor(b, a)
	}

	if debugBlockOpt {
		fmt.Fprintln(os.Stderr, "fuseBlocks", a, b)
	}

	function.Blocks[b.Index] = nil // delete b
	return true
}

// optimizeBlocks() performs some simple block optimizations on a
// completed function: dead block elimination, block fusion, jump
// threading.
func optimizeBlocks(function *Function) {
	deleteUnreachableBlocks(function)

	// Loop until no further progress.
	changed := true
	for changed {
		changed = false

		for _, block := range function.Blocks {
			// f.Blocks will temporarily contain nils to indicate
			// deleted blocks; we remove them at the end.
			if block == nil {
				continue
			}

			// Fuse blocks.  b->c becomes bc.
			if fuseBlocks(function, block) {
				changed = true
			}

			// a->b->c becomes a->c if b contains only a Jump.
			if jumpThreading(function, block) {
				changed = true
				continue // (b was disconnected)
			}
		}
	}
	function.removeNilBlocks()
}
