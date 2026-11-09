package intermediate_representation

// The Panic instruction initiates a panic with value X.
//
// A Panic instruction must be the last instruction of its containing
// BasicBlock, which must have no successors.
//
// NB: 'go panic(x)' and 'defer panic(x)' do not use this instruction;
// they are treated as calls to a built-in function.
//
// Pos() returns the ast.CallExpr.Lparen if this panic was explicit
// in the source.
//
// Example printed form:
//
//	panic t0
type Panic struct {
	anInstruction
	X Value // an interface{}
}

func (panicInstruction *Panic) Operands(rands []*Value) []*Value {
	return append(rands, &panicInstruction.X)
}
