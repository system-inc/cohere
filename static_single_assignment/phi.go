package static_single_assignment

// Phi is a merge: the value of Place at the top of a block, given which predecessor control came from.
//
// Operands holds one entry per entry in the block's predecessors, kept sorted by predecessor block id,
// so ranging it is deterministic and is the order PhiOperandsInOrder has always given.
//
// It was a `map[BlockId]Place`, kept for the lookup "what came from THIS predecessor". A phi has two or
// three operands, where a map's smallest allocation holds eight and a linear scan beats a hash, so the
// map cost 41 MB over 533K phis on a cold ahra run for nothing a short sorted slice cannot answer
// (#p4h0p54). The lookup is `Operands.At` or `Operands.Get`.
type Phi[P any] struct {
	Place    P
	Operands PhiOperands[P]
}

// PhiOperand is the value a phi takes when control arrives from Predecessor.
type PhiOperand[P any] struct {
	Predecessor BlockId
	Place       P
}

// PhiOperandsInOrder returns a phi's predecessor block ids, in ascending block order.
//
// `Phi.Operands` is kept sorted by predecessor block id, so this is its predecessor ids in that order,
// read straight off the slice.
func PhiOperandsInOrder[P any](phi *Phi[P]) []BlockId {
	blocks := make([]BlockId, 0, len(phi.Operands))
	for _, operand := range phi.Operands {
		blocks = append(blocks, operand.Predecessor)
	}
	return blocks
}
