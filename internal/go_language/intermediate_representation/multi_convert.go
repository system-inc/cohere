package intermediate_representation

import "go/types"

// The MultiConvert instruction yields the conversion of value X to type
// Type(). Either X.Type() or Type() must be a type parameter. Each
// type in the type set of X.Type() can be converted to each type in the
// type set of Type().
//
// See the documentation for Convert, ChangeType, SliceToArray, and SliceToArrayPointer
// for the conversions that are permitted.
//
// This operation can fail dynamically (see SliceToArrayPointer).
//
// Example printed form:
//
//	t1 = multiconvert D <- S (t0) [*[2]rune <- []rune | string <- []rune]
type MultiConvert struct {
	register
	X        Value
	from, to types.Type
}

func (convert *MultiConvert) Operands(rands []*Value) []*Value {
	return append(rands, &convert.X)
}
