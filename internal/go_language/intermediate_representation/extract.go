package intermediate_representation

// The Extract instruction yields component Index of Tuple.
//
// This is used to access the results of instructions with multiple
// return values, such as Call, TypeAssert, Next, Receive,
// MapLookup and others.
//
// Example printed form:
//
//	t1 = extract t0 #1
type Extract struct {
	register
	Tuple Value
	Index int
}

func (extract *Extract) Operands(rands []*Value) []*Value {
	return append(rands, &extract.Tuple)
}
