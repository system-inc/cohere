// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// Simple block optimizations to simplify the control flow graph.

// The builder creates several "unreachable" blocks per function where a
// single one (at Blocks[1], say) would do with less garbage.

// markReachable sets gaps=-1 for all blocks reachable from `block`.
//
// Mutates: block and every block reachable from it take the reachable mark in gaps
func markReachable(block *BasicBlock) {
	block.gaps = -1
	for _, successor := range block.Successors {
		if successor.gaps == 0 {
			markReachable(successor)
		}
	}
}

// deleteUnreachableBlocks marks all reachable blocks of `function` and
// eliminates (nils) all others, including possibly cyclic subgraphs.
func deleteUnreachableBlocks(function *Function) {
	const white = 0
	const black = -1
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
	for _, predecessor := range block.Predecessors {
		switch predecessor.Control().(type) {
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
		} else if index == 0 {
			successor.replacePredecessor(block, predecessor)
		} else {
			successor.Predecessors = append(successor.Predecessors, predecessor)
		}
	}
	function.Blocks[block.Index] = nil // delete b
	return true
}

// fuseBlocks attempts to apply the block fusion optimization to block
// a, in which a->b becomes ab if len(a.Successors)==len(b.Predecessors)==1.
// The result is true if the optimization was applied.
//
// Mutates: block takes its successor's instructions and successors
// Mutates: function loses the fused successor from its blocks
func fuseBlocks(function *Function, block *BasicBlock) bool {
	if len(block.Successors) != 1 {
		return false
	}
	successor := block.Successors[0]
	if len(successor.Predecessors) != 1 {
		return false
	}
	if _, ok := block.Instructions[len(block.Instructions)-1].(*Panic); ok {
		// panics aren't simple jumps, they have side effects.
		return false
	}

	// Degenerate &&/|| ops may result in a straight-line CFG
	// containing φ-nodes. (Ideally we'd replace such them with
	// their sole operand but that requires Referrers, built later.)
	if successor.hasPhi() {
		return false // not sound without further effort
	}

	// Eliminate jump at end of A, then copy all of B across.
	block.Instructions = append(block.Instructions[:len(block.Instructions)-1], successor.Instructions...)
	for _, instruction := range successor.Instructions {
		instruction.setBlock(block)
	}

	// A inherits B's successors
	block.Successors = append(block.successorsSpace[:0], successor.Successors...)

	// Fix up Predecessors links of all successors of B.
	for _, inheritedSuccessor := range successor.Successors {
		inheritedSuccessor.replacePredecessor(successor, block)
	}

	function.Blocks[successor.Index] = nil // delete b
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
