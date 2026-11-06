package program

import (
	"fmt"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// ruleContainment is the boundary around one rule on one file. The first panic is recorded and the
// rule hears nothing more about the file; the other rules never notice.
type ruleContainment struct {
	ruleName string
	fileName string
	crash    *RuleCrash
}

// recoverPanic records the panic the deferring call raised, if any. It must be deferred directly, since
// recover answers only in a function called by the deferral itself.
func (containment *ruleContainment) recoverPanic() {
	if recovered := recover(); recovered != nil && containment.crash == nil {
		containment.crash = &RuleCrash{
			RuleName: containment.ruleName,
			FileName: containment.fileName,
			Cause:    fmt.Errorf("%v", recovered),
		}
	}
}

// run calls the rule's Run, its options already bound, inside the boundary.
func (containment *ruleContainment) run(subject rule.Bound, context rule.Context) (listeners rule.Listeners) {
	defer containment.recoverPanic()
	return subject.Run(context)
}
