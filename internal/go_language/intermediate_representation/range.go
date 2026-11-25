package intermediate_representation

// The Range instruction yields an iterator over the domain and range
// of X, which must be a string or map.
//
// Elements are accessed via Next.
//
// Type() returns an opaque and degenerate "rangeIter" type.
//
// Pos() returns the ast.RangeStmt.For.
//
// Example printed form:
//
//	t0 = range "hello":string
type Range struct {
	register
	X Value // string or map
}

func (span *Range) Operands(rands []*Value) []*Value {
	return append(rands, &span.X)
}
