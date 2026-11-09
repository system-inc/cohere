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

// markReachable sets Index=-1 for all blocks reachable from b.
func markReachable(b *BasicBlock) {
	b.gaps = -1
	for _, succ := range b.Successors {
		if succ.gaps == 0 {
			markReachable(succ)
		}
	}
}

// deleteUnreachableBlocks marks all reachable blocks of f and
// eliminates (nils) all others, including possibly cyclic subgraphs.
func deleteUnreachableBlocks(f *Function) {
	const white, black = 0, -1
	// We borrow b.gaps temporarily as the mark bit.
	for _, b := range f.Blocks {
		b.gaps = white
	}
	markReachable(f.Blocks[0])
	if f.Recover != nil {
		markReachable(f.Recover)
	}
	for i, b := range f.Blocks {
		if b.gaps == white {
			for _, c := range b.Successors {
				if c.gaps == black {
					c.removePredecessor(b) // delete white->black edge
				}
			}
			if debugBlockOpt {
				fmt.Fprintln(os.Stderr, "unreachable", b)
			}
			f.Blocks[i] = nil // delete b
		}
	}
	f.removeNilBlocks()
}

// jumpThreading attempts to apply simple jump-threading to block b,
// in which a->b->c become a->c if b is just a Jump.
// The result is true if the optimization was applied.
func jumpThreading(f *Function, b *BasicBlock) bool {
	if b.Index == 0 {
		return false // don't apply to entry block
	}
	if len(b.Instructions) == 0 {
		return false
	}
	for _, pred := range b.Predecessors {
		switch pred.Control().(type) {
		case *ConstantSwitch:
			// don't optimize away the head blocks of switch statements
			return false
		}
	}
	if _, ok := b.Instructions[0].(*Jump); !ok {
		return false // not just a jump
	}
	c := b.Successors[0]
	if c == b {
		return false // don't apply to degenerate jump-to-self.
	}
	if c.hasPhi() {
		return false // not sound without more effort
	}
	for j, a := range b.Predecessors {
		a.replaceSuccessor(b, c)

		// If a now has two edges to c, replace its degenerate If by Jump.
		if len(a.Successors) == 2 && a.Successors[0] == c && a.Successors[1] == c {
			jump := new(Jump)
			jump.setBlock(a)
			a.Instructions[len(a.Instructions)-1] = jump
			a.Successors = a.Successors[:1]
			c.removePredecessor(b)
		} else {
			if j == 0 {
				c.replacePredecessor(b, a)
			} else {
				c.Predecessors = append(c.Predecessors, a)
			}
		}

		if debugBlockOpt {
			fmt.Fprintln(os.Stderr, "jumpThreading", a, b, c)
		}
	}
	f.Blocks[b.Index] = nil // delete b
	return true
}

// fuseBlocks attempts to apply the block fusion optimization to block
// a, in which a->b becomes ab if len(a.Successors)==len(b.Predecessors)==1.
// The result is true if the optimization was applied.
func fuseBlocks(f *Function, a *BasicBlock) bool {
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

	f.Blocks[b.Index] = nil // delete b
	return true
}

// optimizeBlocks() performs some simple block optimizations on a
// completed function: dead block elimination, block fusion, jump
// threading.
func optimizeBlocks(f *Function) {
	deleteUnreachableBlocks(f)

	// Loop until no further progress.
	changed := true
	for changed {
		changed = false

		for _, b := range f.Blocks {
			// f.Blocks will temporarily contain nils to indicate
			// deleted blocks; we remove them at the end.
			if b == nil {
				continue
			}

			// Fuse blocks.  b->c becomes bc.
			if fuseBlocks(f, b) {
				changed = true
			}

			// a->b->c becomes a->c if b contains only a Jump.
			if jumpThreading(f, b) {
				changed = true
				continue // (b was disconnected)
			}
		}
	}
	f.removeNilBlocks()
}
