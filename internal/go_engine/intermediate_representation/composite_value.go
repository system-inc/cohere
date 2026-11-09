package intermediate_representation

import "math/big"

type CompositeValue struct {
	register

	// Bitmap records which elements were explicitly provided. For example, [4]byte{2: x} would have a bitmap of 0010.
	Bitmap big.Int
	// The number of bits set in Bitmap
	// TODO(dh): remove ExplicitCount. we can just query the bitmap.
	ExplicitCount int
	// Dense list of values in the composite literal. Omitted elements are filled in with zero values.
	Values []Value
}

func (value *CompositeValue) Operands(rands []*Value) []*Value {
	for index := range value.Values {
		rands = append(rands, &value.Values[index])
	}
	return rands
}
