package staticcheck_call

import (
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Call is one call a Rule judges.
type Call struct {
	// Context is the check's context, the package the call is in, its type information and its file set, where
	// reports go, in place of the original's analysis pass.
	Context *rule.Context
	// Instruction is the call, a *Call, *Defer or *Go of the engine's own intermediate representation.
	Instruction intermediate_representation.CallInstruction
	// Arguments are the call's arguments, the receiver of a method left out.
	Arguments []*Argument
	// Parent is the function the call is in.
	Parent *intermediate_representation.Function

	// invalids are the messages the Rule marked the call itself with.
	invalids []string
}

// Invalid marks the call invalid, reported over the call with message.
func (call *Call) Invalid(message string) {
	call.invalids = append(call.invalids, message)
}
