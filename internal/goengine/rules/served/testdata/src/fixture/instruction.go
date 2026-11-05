package fixture

// Await's only method marks it an Instruction, and an interface declares no methods of its own, so the file serves
// nothing.
type Instruction interface {
	instructionValue()
}

type Await struct {
	label string
}

func (*Await) instructionValue() {}
