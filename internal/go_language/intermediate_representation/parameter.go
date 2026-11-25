package intermediate_representation

import (
	"go/token"
	"go/types"
)

// A Parameter represents an input parameter of a function.
type Parameter struct {
	node

	name      string
	object    *types.Var // non-nil
	valueType types.Type
	parent    *Function
	referrers []Instruction
}

func (parameter *Parameter) Type() types.Type { return parameter.valueType }

func (parameter *Parameter) Name() string { return parameter.name }

func (parameter *Parameter) Object() types.Object { return parameter.object }

func (parameter *Parameter) Referrers() *[]Instruction { return &parameter.referrers }

func (parameter *Parameter) Pos() token.Pos { return parameter.object.Pos() }

func (parameter *Parameter) Parent() *Function { return parameter.parent }

func (parameter *Parameter) Operands(rands []*Value) []*Value { return rands }
