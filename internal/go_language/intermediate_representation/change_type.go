package intermediate_representation

// The ChangeType instruction applies to X a value-preserving type
// change to Type().
//
// Type changes are permitted:
//   - between a named type and its underlying type.
//   - between two named types of the same underlying type.
//   - between (possibly named) pointers to identical base types.
//   - from a bidirectional channel to a read- or write-channel,
//     optionally adding/removing a name.
//   - between a type (t) and an instance of the type (tσ), i.e.
//     Type() == σ(X.Type()) (or X.Type()== σ(Type())) where
//     σ is the type substitution of Parent().TypeParameters by
//     Parent().TypeArguments.
//
// This operation cannot fail dynamically.
//
// Type changes may to be to or from a type parameter (or both). All
// types in the type set of X.Type() have a value-preserving type
// change to all types in the type set of Type().
//
// Pos() returns the ast.CallExpr.Lparen, if the instruction arose
// from an explicit conversion in the source.
//
// Example printed form:
//
//	t1 = changetype *int <- IntPtr (t0)
type ChangeType struct {
	register
	X Value
}

func (changeType *ChangeType) Operands(rands []*Value) []*Value {
	return append(rands, &changeType.X)
}
