package intermediate_representation

// The Phi instruction represents an SSA φ-node, which combines values
// that differ across incoming control-flow edges and yields a new
// value.  Within a block, all φ-nodes must appear before all non-φ
// nodes.
//
// Pos() returns the position of the && or || for short-circuit
// control-flow joins, or that of the *Allocation for φ-nodes inserted
// during SSA renaming.
//
// Example printed form:
//
//	t2 = phi [0: t0, 1: t1]
type Phi struct {
	register
	Edges []Value // Edges[i] is value for Block().Predecessors[i]
}

func (phi *Phi) Operands(rands []*Value) []*Value {
	operands := rands
	for index := range phi.Edges {
		operands = append(operands, &phi.Edges[index])
	}
	return operands
}
