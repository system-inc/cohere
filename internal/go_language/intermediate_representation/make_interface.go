package intermediate_representation

// MakeInterface constructs an instance of an interface type from a
// value of a concrete type.
//
// Use Program.MethodSets.MethodSet(X.Type()) to find the method-set
// of X, and Program.MethodValue(m) to find the implementation of a method.
//
// To construct the zero value of an interface type T, use:
//
//	NewConstant(constant.MakeNil(), T, pos)
//
// Pos() returns the ast.CallExpr.Lparen, if the instruction arose
// from an explicit conversion in the source.
//
// Example printed form:
//
//	t1 = make interface{} <- int (42:int)
//	t1 = make Stringer <- t0
type MakeInterface struct {
	register
	X Value
}

func (makeInterface *MakeInterface) Operands(rands []*Value) []*Value {
	return append(rands, &makeInterface.X)
}
