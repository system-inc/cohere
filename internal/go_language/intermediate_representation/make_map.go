package intermediate_representation

// The MakeMap instruction creates a new hash-table-based map object
// and yields a value of kind map.
//
// Type() returns a (possibly named) *types.Map.
//
// Pos() returns the ast.CallExpr.Lparen, if created by make(map), or
// the ast.CompositeLit.Lbrack if created by a literal.
//
// Example printed form:
//
//	t1 = make map[string]int t0
//	t1 = make StringIntMap t0
type MakeMap struct {
	register
	Reserve Value // initial space reservation; nil => default
}

func (makeMap *MakeMap) Operands(rands []*Value) []*Value {
	return append(rands, &makeMap.Reserve)
}
