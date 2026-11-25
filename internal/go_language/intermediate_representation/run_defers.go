package intermediate_representation

// The RunDefers instruction pops and invokes the entire stack of
// procedure calls pushed by Defer instructions in this function.
//
// It is legal to encounter multiple 'rundefers' instructions in a
// single control-flow path through a function; this is useful in
// the combined init() function, for example.
//
// Pos() returns NoPos.
//
// Example printed form:
//
//	rundefers
type RunDefers struct {
	anInstruction
}

func (*RunDefers) Operands(rands []*Value) []*Value {
	return rands
}
