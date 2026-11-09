package intermediate_representation

// The MakeChannel instruction creates a new channel object and yields a
// value of kind chan.
//
// Type() returns a (possibly named) *types.Chan.
//
// Pos() returns the ast.CallExpr.Lparen for the make(chan) that
// created it.
//
// Example printed form:
//
//	t1 = make chan int t0
//	t1 = make IntChan t0
type MakeChannel struct {
	register
	Size Value // int; size of buffer; zero => synchronous.
}

func (channel *MakeChannel) Operands(rands []*Value) []*Value {
	return append(rands, &channel.Size)
}
