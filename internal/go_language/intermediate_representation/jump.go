package intermediate_representation

// The Jump instruction transfers control to the sole successor of its
// owning block.
//
// A Jump must be the last instruction of its containing BasicBlock.
//
// Pos() returns NoPos.
//
// Example printed form:
//
//	jump done
type Jump struct {
	anInstruction
}

func (*Jump) Operands(rands []*Value) []*Value {
	return rands
}
