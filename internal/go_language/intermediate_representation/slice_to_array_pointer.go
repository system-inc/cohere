package intermediate_representation

// The SliceToArrayPointer instruction yields the conversion of slice X to
// array pointer.
//
// Pos() returns the ast.CallExpr.Lparen, if the instruction arose
// from an explicit conversion in the source.
//
// Conversion may to be to or from a type parameter. All types in
// the type set of X.Type() must be a slice types that can be converted to
// all types in the type set of Type() which must all be pointer to array
// types.
//
// This operation can fail dynamically if the length of the slice is less
// than the length of the array.
//
// Example printed form:
//
//	t1 = slice to array pointer *[4]byte <- []byte (t0)
type SliceToArrayPointer struct {
	register
	X Value
}

func (pointer *SliceToArrayPointer) Operands(rands []*Value) []*Value {
	return append(rands, &pointer.X)
}
