package intermediate_representation

// The FieldAddress instruction yields the address of Field of *struct X.
//
// The field is identified by its index within the field list of the
// struct type of X.
//
// Dynamically, this instruction panics if X evaluates to a nil
// pointer.
//
// Type() returns a (possibly named) *types.Pointer.
//
// Pos() returns the position of the ast.SelectorExpr.Sel for the
// field, if explicit in the source. For implicit selections, returns
// the position of the inducing explicit selection. If produced for a
// struct literal S{f: e}, it returns the position of the colon; for
// S{e} it returns the start of expression e.
//
// Example printed form:
//
//	t1 = &t0.name [#1]
type FieldAddress struct {
	register
	X     Value // *struct
	Field int   // index into CoreType(CoreType(X.Type()).(*types.Pointer).Elem()).(*types.Struct).Fields
}

func (address *FieldAddress) Operands(rands []*Value) []*Value {
	return append(rands, &address.X)
}
