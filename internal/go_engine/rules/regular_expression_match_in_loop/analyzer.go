// Package regular_expression_match_in_loop is staticcheck's SA6000, regexp.Match, MatchReader or MatchString given a
// constant pattern inside a loop, which compiles the same pattern on every turn, ported onto staticcheck_call, the
// house's translation of the callcheck framework the original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa6000/sa6000.go, whose rules it keeps
// and whose message it reports word for word over the same range, the call. It finds loops with irutil.FindLoops, as
// the original does. The registry runs the port in the original's place, under its name, and the original stays only as
// the oracle: the parity tests beside this file run both over staticcheck's own fixture and the house's. The two differ
// nowhere: staticcheck_call departs from callcheck only where it would panic, on calls the regexp functions are never
// part of, as testdata/src/differences records.
package regular_expression_match_in_loop

import (
	"fmt"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
)

// Check judges every call in a function to a regexp function that compiles its pattern and matches once.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"regexp.Match":       checkMatch("regexp.Match"),
	"regexp.MatchReader": checkMatch("regexp.MatchReader"),
	"regexp.MatchString": checkMatch("regexp.MatchString"),
})

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{Function: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA6000",
	"SA6000",
	"Flags regexp.Match, MatchReader or MatchString given a constant pattern inside a loop: staticcheck's SA6000, "+
		"ported onto staticcheck's intermediate representation.",
	&Listener,
)

// checkMatch is the rule for the function named name, which marks the call when its pattern, its first argument, is
// a constant and the call sits in a loop of its function. A loop that holds a closure holding the call is the
// closure's caller's, so that call is not marked.
func checkMatch(name string) staticcheck_call.Rule {
	return func(call *staticcheck_call.Call) {
		if staticcheck_call.ExtractConstant(call.Arguments[0].Value) == nil {
			return
		}
		if !isInLoop(call.Instruction.Block()) {
			return
		}
		call.Invalid(fmt.Sprintf("calling %s in a loop has poor performance, consider using regexp.Compile", name))
	}
}

// isInLoop is whether block belongs to a loop of the function it is in.
func isInLoop(block *ir.BasicBlock) bool {
	for _, loop := range irutil.FindLoops(block.Parent()) {
		if loop.Has(block) {
			return true
		}
	}
	return false
}
