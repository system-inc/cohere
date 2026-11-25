package intermediate_representation

// An indexMode specifies the (addressing) mode of an index operand.
//
// Addressing mode of an index operation is based on the set of
// underlying types.
// Hasse diagram of the indexMode meet semi-lattice:
//
//	ixVar     ixMap
//	  |          |
//	ixArrVar     |
//	  |          |
//	ixValue      |
//	   \        /
//	  ixInvalid
type indexMode byte

// meet is the address type that is constrained by both x and y.
func (mode indexMode) meet(y indexMode) indexMode {
	if (mode == ixMap || y == ixMap) && mode != y {
		return ixInvalid
	}
	// Use int representation and return min.
	if mode < y {
		return y
	}
	return mode
}
