package intermediate_representation

// anInstruction is a mix-in embedded by all Instructions.
// It provides the implementations of the Block and setBlock methods.
type anInstruction struct {
	node
	id    Id
	block *BasicBlock // the basic block of this instruction
}

func (instruction *anInstruction) setId(id Id) {
	instruction.id = id
}

func (instruction anInstruction) Id() Id {
	return instruction.id
}

// Comment is empty: only the kinds the builder or lifting labels (Allocation, Load, Store, Phi and MakeClosure)
// carry a comment, each in a field and a Comment of its own, so every other instruction is 16 bytes smaller.
func (anInstruction) Comment() string { return "" }

func (instruction *anInstruction) Parent() *Function { return instruction.block.parent }

func (instruction *anInstruction) Block() *BasicBlock { return instruction.block }

func (instruction *anInstruction) setBlock(block *BasicBlock) { instruction.block = block }

func (instruction *anInstruction) Referrers() *[]Instruction { return nil }
