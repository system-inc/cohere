package rule

import (
	"errors"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// IntermediateRepresentationListener is what a Listener hears of the walk over the engine's own intermediate
// representation (internal/go_engine/intermediate_representation), built by the rules'
// build_intermediate_representation pass: a callback for each function, for each instruction of the kinds listed, and
// for each function's exit. The Listener's PackageExit follows this walk.
type IntermediateRepresentationListener struct {
	// Function is called once for each source function of the package, in the order buildir's SrcFuncs lists them,
	// before any of the function's instructions. The function is built by then.
	Function func(context *Context, function *intermediate_representation.Function)
	// Instructions are the kinds Instruction is called for, each written as a typed nil,
	// `(*intermediate_representation.Call)(nil)`.
	Instructions []intermediate_representation.Instruction
	// Instruction is called for each instruction of a kind Instructions lists, in block order, after Function.
	Instruction func(context *Context, instruction intermediate_representation.Instruction)
	// FunctionExit is called once the function's instructions have all been heard.
	FunctionExit func(context *Context, function *intermediate_representation.Function)

	// ReadsValueForExpression is whether a callback reads Function.ValueForExpression, which answers only where the
	// package recorded its expression map. The dispatcher has the rule's analyzer require what marks it a reader of
	// the map, so the package records it wherever the rule runs, and only there.
	ReadsValueForExpression bool
}

// validate refuses a walk declared with none of its callbacks, and kinds and their callback that don't go together,
// as Listener.Validate refuses them on the syntax walk.
func (listener *IntermediateRepresentationListener) validate() error {
	if listener.Function == nil && len(listener.Instructions) == 0 && listener.Instruction == nil &&
		listener.FunctionExit == nil {
		return errors.New("declares the engine's own intermediate representation's walk with none of its callbacks, " +
			"so nothing of the walk would ever call it")
	}
	return validateKinds(
		"instruction",
		"IntermediateRepresentation.Instructions",
		"IntermediateRepresentation.Instruction",
		listener.Instructions,
		listener.Instruction != nil,
	)
}
