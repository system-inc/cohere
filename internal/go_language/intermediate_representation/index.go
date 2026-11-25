package intermediate_representation

// The Index instruction yields element Index of collection X, an array,
// string or type parameter containing an array, a string, a pointer to an,
// array or a slice.
//
// Pos() returns the ast.IndexExpr.Lbrack for the index operation, if
// explicit in the source.
//
// Example printed form:
//
//	t2 = t0[t1]
type Index struct {
	register
	X     Value // array, string or type parameter with types array, *array, slice, or string.
	Index Value // integer index
}

func (index *Index) Operands(rands []*Value) []*Value {
	return append(rands, &index.X, &index.Index)
}
