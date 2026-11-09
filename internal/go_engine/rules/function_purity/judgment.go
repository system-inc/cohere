package function_purity

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// judgment is one package's judging: its pass, its package in our representation, and every function of the package
// whose judging has begun, so a call back into one still being judged answers not pure.
type judgment struct {
	pass      *analysis.Pass
	irPackage *intermediate_representation.Package
	seen      map[*intermediate_representation.Function]bool
}

// isPure is whether function is pure: held so by an IsPure fact, or of this package and judged so here, once, which
// exports the fact. A function with no object, an anonymous one, is never judged, and one of another package that its
// facts do not hold pure is not pure.
func (judging *judgment) isPure(function *intermediate_representation.Function) bool {
	object := function.Object()
	if object == nil {
		return false
	}
	if judging.pass.ImportObjectFact(object, new(IsPure)) {
		return true
	}
	if function.Package != judging.irPackage {
		return false
	}
	if judging.seen[function] {
		return false
	}
	judging.seen[function] = true
	declared, isDeclared := object.(*types.Func)
	if !isDeclared {
		// Invariant: a function's object is the *types.Func it was built from, so any other is a mistake in the
		// representation, which the original's assertion would panic on too.
		panic(fmt.Sprintf("function_purity: %s's object is a %T, not a function", function, object))
	}
	pure := judging.judge(function, declared)
	if pure {
		judging.pass.ExportObjectFact(object, &IsPure{})
	}
	return pure
}

// judge is whether function, declared as declared, is pure by its own body: not a stub, and listed pure or returning
// a result from parameters of basic types only, through a body that calls only pure functions, len and cap, and
// writes, reads and allocates only on its own stack.
func (judging *judgment) judge(function *intermediate_representation.Function, declared *types.Func) bool {
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
		if !isBasic(parameter.Type()) {
			return false
		}
	}
	// A function with no blocks is external, and is not pure.
	if function.Blocks == nil {
		return false
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			if !judging.allows(function, instruction) {
				return false
			}
		}
	}
	return true
}

// allows is whether instruction, in function, leaves function pure.
func (judging *judgment) allows(
	function *intermediate_representation.Function,
	instruction intermediate_representation.Instruction,
) bool {
	switch instruction := instruction.(type) {
	case *intermediate_representation.Call:
		return judging.allowsCall(function, instruction.Common())
	case *intermediate_representation.Defer:
		return judging.allowsCall(function, &instruction.Call)
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
func (judging *judgment) allowsCall(
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
	return judging.isPure(callee)
}

// isBasic is whether a value of valueType holds only basic values: a basic type, or a struct whose fields are all
// basic.
func isBasic(valueType types.Type) bool {
	switch underlying := valueType.Underlying().(type) {
	case *types.Basic:
		return true
	case *types.Struct:
		for field := range underlying.Fields() {
			if !isBasic(field.Type()) {
				return false
			}
		}
		return true
	default:
		return false
	}
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
