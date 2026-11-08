// Package timer_reset_result_used is staticcheck's SA1025, a branch on the result of (*time.Timer).Reset that drains
// a timer's channel, which races with the timer firing anew, ported onto staticcheck's intermediate representation as
// a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1025/sa1025.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call to Reset, where staticcheck's report
// puts it. It names the call and walks the blocks of a branch through honnef.co/go/tools/go/ir/irutil, as the
// original does. The original walks every source function of a package in one run, but keeps nothing from one
// function to the next: each call to Reset is judged by the branches its own result decides. So the port judges one
// function at a time and finds what the original finds, including the original's one finding per branch on the
// result, so a result two ifs read is reported twice. The original stays the oracle: the parity tests beside this file
// run both over the same packages, and they differ nowhere. The one place the port could differ is a call with no
// syntax, where staticcheck's report panics and the port reports nothing; staticcheck builds every call to Reset from
// its call expression, so no such call reaches either.
package timer_reset_result_used

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA1025",
	"Flags a branch on the result of (*time.Timer).Reset that drains a timer's channel: staticcheck's SA1025, "+
		"ported onto its intermediate representation.",
	Check,
)

// message is the original's, word for word.
const message = "it is not possible to use Reset's return value correctly, as there is a race condition between " +
	"draining the channel and the new timer expiring"

// timerChannelType is the type of a time.Timer's channel as types.TypeString writes it with full package paths. An
// alias of it is spelled differently and does not match, which the original intends, since only time.Timer.C matters.
const timerChannelType = "<-chan time.Time"

// Check judges one function: each call to (*time.Timer).Reset whose result an if reads directly, reported once for
// every such if one of whose branches receives from a channel of time.Time.
func Check(context *rule.Context, function *ir.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, isCall := instruction.(*ir.Call)
			if !isCall || !irutil.IsCallTo(call.Common(), "(*time.Timer).Reset") {
				continue
			}
			referrers := call.Referrers()
			if referrers == nil {
				continue
			}
			for _, referrer := range *referrers {
				branch, isIf := referrer.(*ir.If)
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
func drainsInBranch(branch *ir.If) bool {
	found := false
	for _, successor := range branch.Block().Succs {
		if len(successor.Preds) != 1 {
			continue
		}
		irutil.Walk(successor, func(block *ir.BasicBlock) bool {
			// A block the branch's first block does not dominate is past the end of the branch.
			if found || !successor.Dominates(block) {
				return false
			}
			for _, instruction := range block.Instrs {
				receive, isReceive := instruction.(*ir.Recv)
				if isReceive && types.TypeString(receive.Chan.Type(), nil) == timerChannelType {
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
