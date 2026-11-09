// Package timer_reset_result_used is staticcheck's SA1025, a branch on the result of (*time.Timer).Reset that drains
// a timer's channel, which races with the timer firing anew, ported onto the engine's own intermediate
// representation (#tey4xje).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1025/sa1025.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call to Reset, where staticcheck's report
// puts it. It names the call and walks the blocks of a branch through IsCallTo and Walk, irutil's translated onto our
// representation, as the original does through irutil's. The original walks every source function of a package in one
// run, but keeps nothing from one function to the next: each call to Reset is judged by the branches its own result
// decides. So the port judges one function at a time and finds what the original finds, including the original's one
// finding per branch on the result, so a result two ifs read is reported twice. The registry runs the port in the
// original's place, under its name, and the original stays only as the oracle: the parity tests beside this file run
// both over the same packages, and they differ nowhere. The one place the port could differ is a call with no syntax,
// where staticcheck's report panics and the port reports nothing; staticcheck builds every call to Reset from its call
// expression, so no such call reaches either.
package timer_reset_result_used

import (
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA1025",
	"SA1025",
	"Flags a branch on the result of (*time.Timer).Reset that drains a timer's channel: staticcheck's SA1025, "+
		"ported onto the engine's own intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "it is not possible to use Reset's return value correctly, as there is a race condition between " +
	"draining the channel and the new timer expiring"

// timerChannelType is the type of a time.Timer's channel as types.TypeString writes it with full package paths. An
// alias of it is spelled differently and does not match, which the original intends, since only time.Timer.C matters.
const timerChannelType = "<-chan time.Time"

// Check judges one function: each call to (*time.Timer).Reset whose result an if reads directly, reported once for
// every such if one of whose branches receives from a channel of time.Time.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(*intermediate_representation.Call)
			if !isCall || !intermediate_representation.IsCallTo(call.Common(), "(*time.Timer).Reset") {
				continue
			}
			referrers := call.Referrers()
			if referrers == nil {
				continue
			}
			for _, referrer := range *referrers {
				branch, isIf := referrer.(*intermediate_representation.If)
				if !isIf || !drainsInBranch(branch) {
					continue
				}
				source := call.Source()
				if source == nil {
					// A call with no syntax has no place to report; staticcheck's report panics on one.
					continue
				}
				context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
			}
		}
	}
}

// drainsInBranch is whether a block of either branch of branch receives from a channel of time.Time. A branch is the
// blocks its first block dominates, reached from it; a successor with more than one predecessor is where the if's
// paths merge, not a branch of the if, so it is passed over, which also passes over the branches of an if on `x || y`.
func drainsInBranch(branch *intermediate_representation.If) bool {
	found := false
	for _, successor := range branch.Block().Successors {
		if len(successor.Predecessors) != 1 {
			continue
		}
		intermediate_representation.Walk(successor, func(block *intermediate_representation.BasicBlock) bool {
			// A block the branch's first block does not dominate is past the end of the branch.
			if found || !successor.Dominates(block) {
				return false
			}
			for _, instruction := range block.Instructions {
				receive, isReceive := instruction.(*intermediate_representation.Receive)
				if isReceive && types.TypeString(receive.Channel.Type(), nil) == timerChannelType {
					found = true
					return false
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}
