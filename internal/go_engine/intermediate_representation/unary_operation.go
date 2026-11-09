package intermediate_representation

import "go/token"

// The UnaryOperation instruction yields the result of Operator X.
// XOR is bitwise complement.
// SUB is negation.
// NOT is logical negation.
//
// Example printed form:
//
//	t0 = ^x
type UnaryOperation struct {
	register
	Operator token.Token // One of: NOT SUB XOR ! - ^
	X        Value
}

func (operation *UnaryOperation) Operands(rands []*Value) []*Value {
	return append(rands, &operation.X)
}
