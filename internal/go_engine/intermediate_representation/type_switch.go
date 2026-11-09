package intermediate_representation

import "go/types"

type TypeSwitch struct {
	register
	Tag        Value
	Conditions []types.Type
}

func (typeSwitch *TypeSwitch) Operands(rands []*Value) []*Value {
	rands = append(rands, &typeSwitch.Tag)
	return rands
}
