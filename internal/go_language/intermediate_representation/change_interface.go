package intermediate_representation

// ChangeInterface constructs a value of one interface type from a
// value of another interface type known to be assignable to it.
// This operation cannot fail.
//
// Pos() returns the ast.CallExpr.Lparen if the instruction arose from
// an explicit T(e) conversion; the ast.TypeAssertExpr.Lparen if the
// instruction arose from an explicit e.(T) operation; or token.NoPos
// otherwise.
//
// Example printed form:
//
//	t1 = change interface interface{} <- I (t0)
type ChangeInterface struct {
	register
	X Value
}

func (changeInterface *ChangeInterface) Operands(rands []*Value) []*Value {
	return append(rands, &changeInterface.X)
}
