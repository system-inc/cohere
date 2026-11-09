// Package unbuffered_signal_channel is staticcheck's SA1017, an unbuffered channel given to os/signal.Notify, which
// drops a signal that arrives while nothing is receiving, ported onto staticcheck_call, the house's translation of
// the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1017/sa1017.go, whose rule it keeps and
// whose message it reports word for word over the same range, the channel argument's expression. It finds the
// channel's position in staticcheck's knowledge package, a table of standard library argument positions that the
// house ports in a later wave. The original keeps no state from one function to the next, nor does the port. Like the
// original, it knows a channel unbuffered only when the argument is the channel's make itself, or that make
// converted to the send-only type Notify takes, and so misses one that reaches the call through a phi. It departs
// from the original where the port is right: on a size that is a constant with no value the original panics reading
// its kind, and the port judges nothing there; a constant of an integer type always holds a value, so no fixture can
// show it. The registry runs the port in the original's place, under its name, and the original stays only as the
// oracle: the parity tests beside this file run both over staticcheck's own fixture and the house's. The two differ
// nowhere: staticcheck_call departs from callcheck only on calls Notify is never part of, as testdata/src/differences
// records.
package unbuffered_signal_channel

import (
	"go/constant"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to os/signal.Notify.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"os/signal.Notify": checkChannel,
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1017",
	"SA1017",
	"Flags an unbuffered channel given to os/signal.Notify: staticcheck's SA1017, ported onto staticcheck's "+
		"intermediate representation.",
	&Listener,
)

// checkChannel marks the call's channel when it is made unbuffered.
func checkChannel(call *staticcheck_call.Call) {
	channel := call.Arguments[knowledge.Arg("os/signal.Notify.c")]
	if isUnbufferedChannel(channel.Value) {
		channel.Invalid("the channel used with signal.Notify should be buffered")
	}
}

// isUnbufferedChannel is whether value is a channel made with a size of the constant zero, or with none, seen
// through one conversion of its type.
func isUnbufferedChannel(value intermediate_representation.Value) bool {
	made, isMade := value.(*intermediate_representation.MakeChannel)
	if conversion, isConversion := value.(*intermediate_representation.ChangeType); isConversion {
		made, isMade = conversion.X.(*intermediate_representation.MakeChannel)
	}
	if !isMade {
		return false
	}
	size, isConstant := made.Size.(*intermediate_representation.Constant)
	// The guard on a nil value is the departure the package comment names.
	if !isConstant || size.Value == nil || size.Value.Kind() != constant.Int {
		return false
	}
	length, isExact := constant.Int64Val(size.Value)
	return isExact && length == 0
}
