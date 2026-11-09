package intermediate_representation

// The Convert instruction yields the conversion of value X to type
// Type().  One or both of those types is basic (but possibly named).
//
// A conversion may change the value and representation of its operand.
// Conversions are permitted:
//   - between real numeric types.
//   - between complex numeric types.
//   - between string and []byte or []rune.
//   - between pointers and unsafe.Pointer.
//   - between unsafe.Pointer and uintptr.
//   - from (Unicode) integer to (UTF-8) string.
//
// A conversion may imply a type name change also.
//
// Conversions may to be to or from a type parameter. All types in
// the type set of X.Type() can be converted to all types in the type
// set of Type().
//
// This operation cannot fail dynamically.
//
// Conversions of untyped string/number/bool constants to a specific
// representation are eliminated during IR construction.
//
// Pos() returns the ast.CallExpr.Lparen, if the instruction arose
// from an explicit conversion in the source.
//
// Example printed form:
//
//	t1 = convert []byte <- string (t0)
type Convert struct {
	register
	X Value
}

func (convert *Convert) Operands(rands []*Value) []*Value {
	return append(rands, &convert.X)
}
