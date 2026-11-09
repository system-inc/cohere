package intermediate_representation

// The StringLookup instruction yields element Index of collection X, a string.
// Index is an integer expression.
//
// Pos() returns the ast.IndexExpr.Lbrack, if explicit in the source.
//
// Example printed form:
//
//	t3 = StringLookup <uint8> t2 t1
type StringLookup struct {
	register
	X     Value // string
	Index Value // numeric index
}

func (lookup *StringLookup) Operands(rands []*Value) []*Value {
	return append(rands, &lookup.X, &lookup.Index)
}
