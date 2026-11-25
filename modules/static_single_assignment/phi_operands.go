package static_single_assignment

import "slices"

// PhiOperands is a phi's operands, sorted by predecessor block id with at most one entry each.
type PhiOperands[P any] []PhiOperand[P]

// index is where predecessor's entry is, or where it would go, and whether it is there.
func (operands PhiOperands[P]) index(predecessor BlockId) (int, bool) {
	for index, operand := range operands {
		if operand.Predecessor >= predecessor {
			return index, operand.Predecessor == predecessor
		}
	}
	return len(operands), false
}

// Get is the operand from predecessor, and whether there is one.
func (operands PhiOperands[P]) Get(predecessor BlockId) (P, bool) {
	if index, found := operands.index(predecessor); found {
		return operands[index].Place, true
	}
	var none P
	return none, false
}

// At is the operand from predecessor, or the zero place when there is none, as the map's index was.
func (operands PhiOperands[P]) At(predecessor BlockId) P {
	place, _ := operands.Get(predecessor)
	return place
}

// Set makes place the operand from predecessor, replacing one already there.
func (operands *PhiOperands[P]) Set(predecessor BlockId, place P) {
	index, found := operands.index(predecessor)
	if found {
		(*operands)[index].Place = place
		return
	}
	*operands = slices.Insert(*operands, index, PhiOperand[P]{Predecessor: predecessor, Place: place})
}

// Delete removes the operand from predecessor, if there is one.
func (operands *PhiOperands[P]) Delete(predecessor BlockId) {
	if index, found := operands.index(predecessor); found {
		*operands = slices.Delete(*operands, index, index+1)
	}
}
