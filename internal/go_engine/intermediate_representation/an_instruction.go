package intermediate_representation

// anInstruction is a mix-in embedded by all Instructions.
// It provides the implementations of the Block and setBlock methods.
type anInstruction struct {
	node
	id      Id
	block   *BasicBlock // the basic block of this instruction
	comment string
}

func (instruction *anInstruction) setId(id Id) {
	instruction.id = id
}

func (instruction anInstruction) Id() Id {
	return instruction.id
}

func (instruction anInstruction) Comment() string {
	return instruction.comment
}

func (instruction *anInstruction) Parent() *Function { return instruction.block.parent }

func (instruction *anInstruction) Block() *BasicBlock { return instruction.block }

func (instruction *anInstruction) setBlock(block *BasicBlock) { instruction.block = block }

func (instruction *anInstruction) Referrers() *[]Instruction { return nil }
