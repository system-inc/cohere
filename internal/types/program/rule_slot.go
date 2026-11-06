package program

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ruleSlot is one rule on one worker: what dispatching the rule needs that does not change between files.
type ruleSlot struct {
	name   string
	timing *RuleTiming

	// containment is the rule's boundary on the file being dispatched, made afresh each time the rule is
	// offered a file.
	containment ruleContainment

	report     func(rule.Diagnostic)
	recordNote func(key string)

	// programView is the rule's view of the program, pointed at each file this worker dispatches and
	// released when the file ends.
	programView rule.ProgramViewSlot
}

// hear calls one of the rule's listeners inside its containment. Once the rule has crashed on this file its
// listeners are skipped, because a rule past its panic is in a state nobody tested.
func (slot *ruleSlot) hear(meter *workerMeter, listener func(node *ast.Node), node *ast.Node) {
	if slot.containment.crash != nil {
		return
	}
	defer slot.containment.recoverPanic()
	meter.call(slot.timing, listener, node)
}
