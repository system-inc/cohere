package intermediate_representation

// The MapLookup instruction yields element Index of collection X, a map.
// Index is the appropriate key type.
//
// If CommaOk, the result is a 2-tuple of the value above and a
// boolean indicating the result of a map membership test for the key.
// The components of the tuple are accessed using Extract.
//
// Pos() returns the ast.IndexExpr.Lbrack, if explicit in the source.
//
// Example printed form:
//
//	t2 = t0[t1]
//	t5 = t3[t4],ok
type MapLookup struct {
	register
	X       Value // map
	Index   Value // key-typed index
	CommaOk bool  // return a value,ok pair
}

func (lookup *MapLookup) Operands(rands []*Value) []*Value {
	return append(rands, &lookup.X, &lookup.Index)
}
