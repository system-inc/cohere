package intermediate_representation

// The Slice instruction yields a slice of an existing string, slice
// or *array X between optional integer bounds Low and High.
//
// Dynamically, this instruction panics if X evaluates to a nil *array
// pointer.
//
// Type() returns string if the type of X was string, otherwise a
// *types.Slice with the same element type as X.
//
// Pos() returns the ast.SliceExpr.Lbrack if created by a x[:] slice
// operation, the ast.CompositeLit.Lbrace if created by a literal, or
// NoPos if not explicit in the source (e.g. a variadic argument slice).
//
// Example printed form:
//
//	t2 = slice t0[t1:]
type Slice struct {
	register
	X                  Value // slice, string, or *array
	Low, High, Maximum Value // each may be nil
}

func (slice *Slice) Operands(rands []*Value) []*Value {
	return append(rands, &slice.X, &slice.Low, &slice.High, &slice.Maximum)
}
