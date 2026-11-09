package intermediate_representation

import "go/token"

// The BinaryOperation instruction yields the result of binary operation X Operator Y.
//
// Pos() returns the ast.BinaryExpr.OpPos, if explicit in the source.
//
// Example printed form:
//
//	t1 = t0 + 1:int
type BinaryOperation struct {
	register
	// One of:
	// `ADD` `SUB` `MUL` `QUO` `REM`              + - * / %
	// `AND` `OR` `XOR` `SHL` `SHR` `AND_NOT`     & | ^ << >> &^
	// `EQL` `NEQ` `LSS` `LEQ` `GTR` `GEQ`        == != < <= > >=
	Operator token.Token
	X, Y     Value
}

func (operation *BinaryOperation) Operands(rands []*Value) []*Value {
	return append(rands, &operation.X, &operation.Y)
}
