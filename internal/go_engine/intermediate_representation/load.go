package intermediate_representation

// The Load instruction loads a value from a memory address.
//
// For implicit memory loads, Pos() returns the position of the
// most closely associated source-level construct; the details are not
// specified.
//
// Example printed form:
//
//	t2 = Load <int> t1
type Load struct {
	register
	X Value
}

func (load *Load) Operands(rands []*Value) []*Value {
	return append(rands, &load.X)
}
