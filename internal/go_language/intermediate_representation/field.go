package intermediate_representation

// The Field instruction yields the Field of struct X.
//
// The field is identified by its index within the field list of the
// struct type of X; by using numeric indices we avoid ambiguity of
// package-local identifiers and permit compact representations.
//
// Pos() returns the position of the ast.SelectorExpr.Sel for the
// field, if explicit in the source. For implicit selections, returns
// the position of the inducing explicit selection.
//
// Example printed form:
//
//	t1 = t0.name [#1]
type Field struct {
	register
	X     Value // struct
	Field int   // index into CoreType(X.Type()).(*types.Struct).Fields
}

func (field *Field) Operands(rands []*Value) []*Value {
	return append(rands, &field.X)
}
