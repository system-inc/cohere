package staticcheck_call

import (
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Call is one call a Rule judges.
type Call struct {
	// Context is the check's context, the package the call is in, its type information and its file set, where
	// reports go, in place of the original's analysis pass.
	Context *rule.Context
	// Instruction is the call, a *ir.Call, *ir.Defer or *ir.Go.
	Instruction ir.CallInstruction
	// Arguments are the call's arguments, the receiver of a method left out.
	Arguments []*Argument
	// Parent is the function the call is in.
	Parent *ir.Function

	// invalids are the messages the Rule marked the call itself with.
	invalids []string
}

// Invalid marks the call invalid, reported over the call with message.
func (call *Call) Invalid(message string) {
	call.invalids = append(call.invalids, message)
}
