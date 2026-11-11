package function_purity

import (
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/type_predicates"
)

// callees is what judging a function's body asks of each function it calls statically: whether that one is pure.
// The push walk answers from the facts and judges a function of its own package as it is met; the resolver's walk
// answers from what it computed, on demand.
type callees interface {
	isPure(function *intermediate_representation.Function) bool
}

// judge is whether function, declared as declared, is pure by its own body: not a stub, and listed pure or returning
// a result from parameters of basic types only, through a body that calls only functions asking judges pure, len and
// cap, and writes, reads and allocates only on its own stack.
func judge(asking callees, function *intermediate_representation.Function, declared *types.Func) bool {
	if intermediate_representation.IsStub(function) {
		return false
	}
	if _, listed := pureStandardLibrary[declared.FullName()]; listed {
		return true
	}
	if function.Signature.Results().Len() == 0 {
		// A function with no results is empty or does work that cannot be seen, as under another build's tags, so it
		// is not pure.
		return false
	}
	for _, parameter := range function.Parameters {
		if !type_predicates.OnlyBasicValues(parameter.Type()) {
			return false
		}
	}
	// A function with no blocks is external, and is not pure.
	if function.Blocks == nil {
		return false
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if !allows(asking, function, instruction) {
				return false
			}
		}
	}
	return true
}

// allows is whether instruction, in function, leaves function pure.
func allows(
	asking callees,
	function *intermediate_representation.Function,
	instruction intermediate_representation.Instruction,
) bool {
	switch instruction := instruction.(type) {
	case *intermediate_representation.Call:
		return allowsCall(asking, function, instruction.Common())
	case *intermediate_representation.Defer:
		return allowsCall(asking, function, &instruction.Call)
	case *intermediate_representation.Select, *intermediate_representation.Send, *intermediate_representation.Go,
		*intermediate_representation.Panic:
		return false
	case *intermediate_representation.Store:
		return isStackAddress(instruction.Address)
	case *intermediate_representation.FieldAddress:
		return isStackAddress(instruction.X)
	case *intermediate_representation.Allocation:
		return !instruction.Heap
	case *intermediate_representation.Load:
		return isStackAddress(instruction.X)
	default:
		return true
	}
}

// allowsCall is whether a call in function leaves it pure: a static call to function itself or to a pure function, or
// a call of len or cap. A call through an interface, or to any other builtin, does not.
func allowsCall(
	asking callees,
	function *intermediate_representation.Function,
	common *intermediate_representation.CallCommon,
) bool {
	if common.IsInvoke() {
		return false
	}
	if builtin, isBuiltin := common.Value.(*intermediate_representation.Builtin); isBuiltin {
		return builtin.Name() == "len" || builtin.Name() == "cap"
	}
	callee := common.StaticCallee()
	if callee == function {
		return true
	}
	if callee == nil {
		return false
	}
	return asking.isPure(callee)
}

// isStackAddress is whether value is the address of a variable on the function's own stack, or of a field of one.
func isStackAddress(value intermediate_representation.Value) bool {
	switch value := value.(type) {
	case *intermediate_representation.Allocation:
		return !value.Heap
	case *intermediate_representation.FieldAddress:
		return isStackAddress(value.X)
	default:
		return false
	}
}
