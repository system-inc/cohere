package intermediate_representation

import "github.com/system-inc/go-types/types"

type TypeSwitch struct {
	register
	Tag        Value
	Conditions []types.Type
}

func (typeSwitch *TypeSwitch) Operands(rands []*Value) []*Value {
	return append(rands, &typeSwitch.Tag)
}
