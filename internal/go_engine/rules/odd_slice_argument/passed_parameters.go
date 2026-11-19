package odd_slice_argument

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// passedParameter is a slice parameter of the calling function passed as it is to a parameter of the callee.
type passedParameter struct {
	parameter       *intermediate_representation.Parameter
	calleeParameter *types.Var
}

// passedParameters is each slice parameter call passes as it is to callee, with the callee's parameter it is passed
// to: the signature's parameter, not the callee's Parameters, which a function of another package, built from export
// data, does not hold. A method's receiver is passed to no parameter.
func passedParameters(
	call *intermediate_representation.Call,
	callee *intermediate_representation.Function,
) []passedParameter {
	var passed []passedParameter
	for argumentIndex, argument := range call.Call.Arguments {
		parameterIndex := argumentIndex
		if callee.Signature.Recv() != nil {
			if argumentIndex == 0 {
				continue
			}
			parameterIndex--
		}
		parameter, isParameter := argument.(*intermediate_representation.Parameter)
		if !isParameter || !typeutil.All(parameter.Type(), typeutil.IsSlice) {
			continue
		}
		passed = append(passed, passedParameter{parameter: parameter,
			calleeParameter: callee.Signature.Params().At(parameterIndex)})
	}
	return passed
}
