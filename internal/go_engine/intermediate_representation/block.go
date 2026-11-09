// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"fmt"
	"go/ast"
)

// This file implements the BasicBlock type.

// addEdge adds a control-flow graph edge from from to to.
func addEdge(from, to *BasicBlock) {
	from.Successors = append(from.Successors, to)
	to.Predecessors = append(to.Predecessors, from)
}

// Control returns the last instruction in the block.
func (block *BasicBlock) Control() Instruction {
	if len(block.Instructions) == 0 {
		return nil
	}
	return block.Instructions[len(block.Instructions)-1]
}

// Parent returns the function that contains block b.
func (block *BasicBlock) Parent() *Function { return block.parent }

func (block *BasicBlock) Reaches(basicBlock *BasicBlock) bool {
	return block.Scc.reachable.Bit(basicBlock.Scc.Index) != 0
}

// String returns a human-readable label of this block.
// It is not guaranteed unique within the function.
func (block *BasicBlock) String() string {
	return fmt.Sprintf("%d", block.Index)
}

// emit appends an instruction to the current basic block.
// If the instruction defines a Value, it is returned.
func (block *BasicBlock) emit(instruction Instruction, source ast.Node) Value {
	instruction.setSource(source)
	instruction.setBlock(block)
	block.Instructions = append(block.Instructions, instruction)
	value, _ := instruction.(Value)
	return value
}

// predecessorIndex returns the i such that b.Predecessors[i] == `basicBlock` or panics if
// there is none.
func (block *BasicBlock) predecessorIndex(basicBlock *BasicBlock) int {
	for index, pred := range block.Predecessors {
		if pred == basicBlock {
			return index
		}
	}
	panic(fmt.Sprintf("no edge %s -> %s", basicBlock, block))
}

// hasPhi returns true if b.Instructions contains φ-nodes.
func (block *BasicBlock) hasPhi() bool {
	_, ok := block.Instructions[0].(*Phi)
	return ok
}

func (block *BasicBlock) Phis() []Instruction {
	return block.phis()
}

// phis returns the prefix of b.Instructions containing all the block's φ-nodes.
func (block *BasicBlock) phis() []Instruction {
	for index, instr := range block.Instructions {
		if _, ok := instr.(*Phi); !ok {
			return block.Instructions[:index]
		}
	}
	return nil // unreachable in well-formed blocks
}

// replacePred replaces all occurrences of p in b's predecessor list with q.
// Ordinarily there should be at most one.
func (block *BasicBlock) replacePredecessor(oldPredecessor, newPredecessor *BasicBlock) {
	for index, pred := range block.Predecessors {
		if pred == oldPredecessor {
			block.Predecessors[index] = newPredecessor
		}
	}
}

// replaceSucc replaces all occurrences of p in b's successor list with q.
// Ordinarily there should be at most one.
func (block *BasicBlock) replaceSuccessor(oldSuccessor, newSuccessor *BasicBlock) {
	for index, succ := range block.Successors {
		if succ == oldSuccessor {
			block.Successors[index] = newSuccessor
		}
	}
}

// removePred removes all occurrences of `basicBlock` in b's
// predecessor list and φ-nodes.
// Ordinarily there should be at most one.
func (block *BasicBlock) removePredecessor(basicBlock *BasicBlock) {
	phis := block.phis()

	// We must preserve edge order for φ-nodes.
	keptCount := 0
	for predecessorIndex, pred := range block.Predecessors {
		if pred != basicBlock {
			block.Predecessors[keptCount] = block.Predecessors[predecessorIndex]
			// Strike out φ-edge too.
			for _, instr := range phis {
				phi := instr.(*Phi)
				phi.Edges[keptCount] = phi.Edges[predecessorIndex]
			}
			keptCount++
		}
	}
	// Nil out b.Predecessors[j:] and φ-edges[j:] to aid GC.
	for predecessorIndex := keptCount; predecessorIndex < len(block.Predecessors); predecessorIndex++ {
		block.Predecessors[predecessorIndex] = nil
		for _, instr := range phis {
			instr.(*Phi).Edges[predecessorIndex] = nil
		}
	}
	block.Predecessors = block.Predecessors[:keptCount]
	for _, instr := range phis {
		phi := instr.(*Phi)
		phi.Edges = phi.Edges[:keptCount]
	}
}
