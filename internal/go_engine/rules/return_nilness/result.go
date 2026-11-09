package return_nilness

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// Result is the nilness of every function's results the facts hold, this package's and its imports', keyed as
// staticcheck's nilness.Result keys them.
type Result struct {
	functions map[*types.Func][]ValueNilness
}

// Nilness is the nilness of the result at index result of function: {_ NeverNil} for a result that cannot be nil,
// {MaybeNil MaybeNil} for a function the facts hold nothing of, and otherwise what they hold, normalized.
func (judged *Result) Nilness(function *types.Func, result int) ValueNilness {
	resultType := function.Signature().Results().At(result).Type()
	if !typeutil.MaybePointerLike(resultType) {
		return ValueNilness{Outer: NeverNil}
	}
	if len(judged.functions[function]) == 0 {
		return ValueNilness{Inner: MaybeNil, Outer: MaybeNil}
	}

	return normalize(judged.functions[function][result], resultType)
}

// normalize fills what value leaves unknown with MaybeNil, and Inner too for a value of valueType that is no interface,
// where Inner means nothing.
func normalize(value ValueNilness, valueType types.Type) ValueNilness {
	if value.Inner == 0 || !types.IsInterface(valueType) {
		value.Inner = MaybeNil
	}
	if value.Outer == 0 {
		value.Outer = MaybeNil
	}
	return value
}
