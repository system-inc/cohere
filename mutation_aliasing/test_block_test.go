package mutation_aliasing

import ssa "github.com/system-inc/cohere/static_single_assignment"

type testBlock struct {
	id            ssa.BlockId
	instructions  []*testInstruction
	successors    []ssa.BlockId
	returns       *testPlace
	predecessors  []ssa.BlockId
	phis          []*ssa.Phi[testPlace]
	terminalOrder ssa.EvaluationOrder
}

// add appends an instruction to a block.
func (block *testBlock) add(instruction *testInstruction) *testInstruction {
	block.instructions = append(block.instructions, instruction)
	return instruction
}
