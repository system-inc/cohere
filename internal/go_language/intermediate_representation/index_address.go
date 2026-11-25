package intermediate_representation

// The IndexAddress instruction yields the address of the element at
// index Index of collection X.  Index is an integer expression.
//
// The elements of maps and strings are not addressable; use Lookup (map),
// Index (string), or MapUpdate instead.
//
// Dynamically, this instruction panics if X evaluates to a nil *array
// pointer.
//
// Type() returns a (possibly named) *types.Pointer.
//
// Pos() returns the ast.IndexExpr.Lbrack for the index operation, if
// explicit in the source.
//
// Example printed form:
//
//	t2 = &t0[t1]
type IndexAddress struct {
	register
	X     Value // *array, slice or type parameter with types array, *array, or slice.
	Index Value // numeric index
}

func (address *IndexAddress) Operands(rands []*Value) []*Value {
	return append(rands, &address.X, &address.Index)
}
