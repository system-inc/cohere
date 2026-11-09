package intermediate_representation

// The MakeSlice instruction yields a slice of length Length backed by a
// newly allocated array of length Capacity.
//
// Both Length and Capacity must be non-nil Values of integer type.
//
// (Allocation(types.Array) followed by Slice will not suffice because
// Allocation can only create arrays of constant length.)
//
// Type() returns a (possibly named) *types.Slice.
//
// Pos() returns the ast.CallExpr.Lparen for the make([]T) that
// created it.
//
// Example printed form:
//
//	t2 = make []string t1 t0
//	t2 = make StringSlice t1 t0
type MakeSlice struct {
	register
	Length   Value
	Capacity Value
}

func (slice *MakeSlice) Operands(rands []*Value) []*Value {
	return append(rands, &slice.Length, &slice.Capacity)
}
