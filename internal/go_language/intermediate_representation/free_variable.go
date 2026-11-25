package intermediate_representation

import "go/types"

// A FreeVariable represents a free variable of the function to which it
// belongs.
//
// FreeVariables are used to implement anonymous functions, whose free
// variables are lexically captured in a closure formed by
// MakeClosure.  The value of such a free var is an Allocation or another
// FreeVariable and is considered a potentially escaping heap address, with
// pointer type.
//
// FreeVariables are also used to implement bound method closures.  Such a
// free var represents the receiver value and may be of any type that
// has concrete methods.
//
// Pos() returns the position of the value that was captured, which
// belongs to an enclosing function.
type FreeVariable struct {
	node

	name      string
	valueType types.Type
	parent    *Function
	referrers []Instruction

	// Transiently needed during building.
	outer Value // the Value captured from the enclosing context.
}

func (variable *FreeVariable) Type() types.Type { return variable.valueType }

func (variable *FreeVariable) Name() string { return variable.name }

func (variable *FreeVariable) Referrers() *[]Instruction { return &variable.referrers }

func (variable *FreeVariable) Parent() *Function { return variable.parent }

func (variable *FreeVariable) Operands(rands []*Value) []*Value { return rands }
