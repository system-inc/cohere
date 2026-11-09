package intermediate_representation

// The Send instruction sends X on channel Channel.
//
// Pos() returns the ast.SendStmt.Arrow, if explicit in the source.
//
// Example printed form:
//
//	send t0 <- t1
type Send struct {
	anInstruction
	Channel, X Value
}

func (send *Send) Operands(rands []*Value) []*Value {
	return append(rands, &send.Channel, &send.X)
}
