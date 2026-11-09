package intermediate_representation

// The Unreachable pseudo-instruction signals that execution cannot
// continue after the preceding function call because it terminates
// the process.
//
// An Unreachable instruction must be the last instruction of its
// containing BasicBlock, which must have no successors.
//
// Example printed form:
//
//	Unreachable
type Unreachable struct {
	anInstruction
}

func (*Unreachable) Operands(rands []*Value) []*Value {
	return rands
}
