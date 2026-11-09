package odd_slice_argument

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// facts is where the push walk reads whether a parameter is marked and marks one: the pass's evenElements facts, as
// the original reads and exports them, or a map of the package's own marks with every other package's asked of the
// resolver.
type facts interface {
	marked(parameter *types.Var) bool
	mark(parameter types.Object)
}

// pushWalk marks one package's parameters the original's way: every source function's own checks first, then, in
// buildir's order with one seen set, each parameter a function passes on to a marked one.
type pushWalk struct {
	facts facts
}

// markLengthChecks marks each slice parameter a function checks on every path for an even length.
func (walk *pushWalk) markLengthChecks(functions []*intermediate_representation.Function) {
	for _, function := range functions {
		for _, parameter := range directChecks(function) {
			walk.facts.mark(parameter.Object())
		}
	}
}

// markIndirectLengthChecks marks each slice parameter a function passes on every path, as it is, to a parameter
// already marked, following the calls into functions of the same package first, so a chain of them is marked from
// its end. A call into a function already begun reads its marks as far as they have come.
func (walk *pushWalk) markIndirectLengthChecks(functions []*intermediate_representation.Function) {
	seen := map[*intermediate_representation.Function]bool{}
	var visit func(function *intermediate_representation.Function)
	visit = func(function *intermediate_representation.Function) {
		if seen[function] {
			return
		}
		seen[function] = true
		for _, block := range function.Blocks {
			if !dominatesEveryReturn(function, block) {
				continue
			}
			for _, instruction := range block.Instructions {
				call, isCall := instruction.(*intermediate_representation.Call)
				if !isCall {
					continue
				}
				callee := call.Call.StaticCallee()
				if callee == nil {
					continue
				}
				if callee.Package == function.Package || callee.Package == nil {
					visit(callee)
				}
				for _, passed := range passedParameters(call, callee) {
					if walk.facts.marked(passed.calleeParameter) {
						walk.facts.mark(passed.parameter.Object())
					}
				}
			}
		}
	}
	for _, function := range functions {
		visit(function)
	}
}

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
