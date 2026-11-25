package intermediate_representation

// The MapUpdate instruction updates the association of Map[Key] to
// Value.
//
// Pos() returns the ast.KeyValueExpr.Colon or ast.IndexExpr.Lbrack,
// if explicit in the source.
//
// Example printed form:
//
//	t0[t1] = t2
type MapUpdate struct {
	anInstruction
	Map   Value
	Key   Value
	Value Value
}

func (update *MapUpdate) Operands(rands []*Value) []*Value {
	return append(rands, &update.Map, &update.Key, &update.Value)
}
