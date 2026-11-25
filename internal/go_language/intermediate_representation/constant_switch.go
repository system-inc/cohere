package intermediate_representation

type ConstantSwitch struct {
	anInstruction
	Tag Value
	// Constant branch conditions. A nil Value denotes the (implicit
	// or explicit) default branch.
	Conditions []Value
}

func (constantSwitch *ConstantSwitch) Operands(rands []*Value) []*Value {
	operands := append(rands, &constantSwitch.Tag)
	for index := range constantSwitch.Conditions {
		operands = append(operands, &constantSwitch.Conditions[index])
	}
	return operands
}
