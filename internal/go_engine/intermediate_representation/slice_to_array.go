package intermediate_representation

// The SliceToArray instruction yields the conversion of slice X to
// array.
//
// Pos() returns the ast.CallExpr.Lparen, if the instruction arose
// from an explicit conversion in the source.
//
// Example printed form:
//
//	t2 = SliceToArray <[4]byte> t1
type SliceToArray struct {
	register
	X Value
}

func (array *SliceToArray) Operands(rands []*Value) []*Value {
	return append(rands, &array.X)
}
