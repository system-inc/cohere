package allowed

// A sum type's variants, whose every method only marks them members, stay beside their interface and beside the one
// type with real methods here, Program, since a marker method carries no behavior to find.
type Instruction interface {
	instructionValue()
}

type Await struct {
	label string
}

func (*Await) instructionValue() {}

// A comment inside the body says why it is empty, and keeps it empty.
type Sleep struct {
	seconds int
}

func (Sleep) instructionValue() {
	// Sleep is an instruction, and saying so is all this method does.
}

// A generic variant is marked through its type parameter like any other.
type Jump[Target any] struct {
	target Target
}

func (*Jump[Target]) instructionValue() {}

type Program struct {
	instructions []Instruction
}

func (program *Program) Append(instruction Instruction) {
	program.instructions = append(program.instructions, instruction)
}
