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

// passedParameters is each slice parameter call passes as it is to callee, with the parameter it is passed to, named
// as declaredParameter names it. A method's receiver is passed to no parameter.
func passedParameters(
	call *intermediate_representation.Call,
	callee *intermediate_representation.Function,
) []passedParameter {
	var passed []passedParameter
	for argumentIndex, argument := range call.Call.Arguments {
		parameter, isParameter := argument.(*intermediate_representation.Parameter)
		if !isParameter || !typeutil.All(parameter.Type(), typeutil.IsSlice) {
			continue
		}
		calleeParameter, isPassed := declaredParameter(callee, argumentIndex)
		if !isPassed {
			continue
		}
		passed = append(passed, passedParameter{parameter: parameter, calleeParameter: calleeParameter})
	}
	return passed
}

// declaredParameter is the parameter the argument at argumentIndex of a call to callee is passed to, and whether there
// is one, a receiver being none: the parameter at that place of the function callee is, or, for an instantiation of a
// generic function or a wrapper of a method, of the declared function it stands for, as that function declares it.
//
// It is never read off a stand-in's own signature. The representation makes an instantiation's signature canonical by
// type identity, so of two generic functions whose instantiations have identical signatures, the one instantiated
// second carries the first's parameters, and which is first is an order of building (instantiate.go). staticcheck
// reads and makes its marks there, so a check one of the two makes marks the other's parameter, or is read for a slice
// passed to the other, and it reports a slice by a check its function never makes, under whichever function's
// parameter name that order gave. And an instantiation whose parameter's type names a type parameter carries a
// parameter no function declares, which no check or pass ever marks, though every instantiation's slice reaches the
// generic function's check. A call names the declared function and a place, which no order changes, so the mark is
// asked of that: the port departs from the original at both, in every case toward what the declared function does
// (testdata/src/instance).
func declaredParameter(callee *intermediate_representation.Function, argumentIndex int) (*types.Var, bool) {
	parameterIndex := argumentIndex
	if callee.Signature.Recv() != nil {
		if argumentIndex == 0 {
			return nil, false
		}
		parameterIndex--
	}
	declared, standsForDeclared := callee.Object().(*types.Func)
	if !standsForDeclared {
		// A function literal, which declares its parameters itself and stands for nothing.
		return callee.Signature.Params().At(parameterIndex), true
	}
	parameters := declared.Origin().Signature().Params()
	// A method expression's thunk takes the method's receiver as its first parameter, before the method's own.
	parameterIndex -= callee.Signature.Params().Len() - parameters.Len()
	if parameterIndex < 0 {
		return nil, false
	}
	return parameters.At(parameterIndex), true
}
