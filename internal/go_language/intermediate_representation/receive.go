package intermediate_representation

// The Receive instruction receives from channel Channel.
//
// If CommaOk, the result is a 2-tuple of the value above
// and a boolean indicating the success of the receive.  The
// components of the tuple are accessed using Extract.
//
// Pos() returns the ast.UnaryExpr.OpPos, if explicit in the source.
// For receive operations implicit in ranging over a channel,
// Pos() returns the ast.RangeStmt.For.
//
// Example printed form:
//
//	t2 = Receive <int> t1
//	t3 = Receive <(int, bool)> t1
type Receive struct {
	register
	Channel Value
	CommaOk bool
}

func (receive *Receive) Operands(rands []*Value) []*Value {
	return append(rands, &receive.Channel)
}
