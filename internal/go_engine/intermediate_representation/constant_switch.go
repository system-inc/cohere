package intermediate_representation

type ConstantSwitch struct {
	anInstruction
	Tag Value
	// Constant branch conditions. A nil Value denotes the (implicit
	// or explicit) default branch.
	Conditions []Value
}

func (constantSwitch *ConstantSwitch) Operands(rands []*Value) []*Value {
	rands = append(rands, &constantSwitch.Tag)
	for index := range constantSwitch.Conditions {
		rands = append(rands, &constantSwitch.Conditions[index])
	}
	return rands
}
