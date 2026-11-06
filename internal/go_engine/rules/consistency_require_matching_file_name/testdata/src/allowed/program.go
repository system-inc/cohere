package allowed

// A sum type's variants, whose every method only marks them members, serve nothing, so this file serves Program
// alone and is named for it.
type Instruction interface {
	instructionValue()
}

type Await struct {
	label string
}

func (*Await) instructionValue() {}

type Sleep struct {
	seconds int
}

func (Sleep) instructionValue() {
	// Sleep is an instruction, and saying so is all this method does.
}

type Program struct {
	instructions []Instruction
}

func (program *Program) Append(instruction Instruction) {
	program.instructions = append(program.instructions, instruction)
}
