package intermediate_representation

// The Return instruction returns values and control back to the calling
// function.
//
// len(Results) is always equal to the number of results in the
// function's signature.
//
// If len(Results) > 1, Return returns a tuple value with the specified
// components which the caller must access using Extract instructions.
//
// There is no instruction to return a ready-made tuple like those
// returned by a "value,ok"-mode TypeAssert, MapLookup or Receive or
// a tail-call to a function with multiple result parameters.
//
// Return must be the last instruction of its containing BasicBlock.
// Such a block has no successors.
//
// Pos() returns the ast.ReturnStmt.Return, if explicit in the source.
//
// Example printed form:
//
//	return
//	return t0, t1
type Return struct {
	anInstruction
	Results []Value
}

func (ret *Return) Operands(rands []*Value) []*Value {
	operands := rands
	for index := range ret.Results {
		operands = append(operands, &ret.Results[index])
	}
	return operands
}
