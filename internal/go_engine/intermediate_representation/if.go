package intermediate_representation

// The If instruction transfers control to one of the two successors
// of its owning block, depending on the boolean Condition: the first if
// true, the second if false.
//
// An If instruction must be the last instruction of its containing
// BasicBlock.
//
// Pos() returns the *ast.IfStmt, if explicit in the source.
//
// Example printed form:
//
//	if t0 goto done else body
type If struct {
	anInstruction
	Condition Value
}

func (instr *If) Operands(rands []*Value) []*Value {
	return append(rands, &instr.Condition)
}
