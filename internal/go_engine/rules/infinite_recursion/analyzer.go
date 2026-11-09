// Package infinite_recursion is staticcheck's SA5007, a function that calls itself on every path to its end, so it
// recurses until the stack runs out, ported onto the engine's own intermediate representation (#tey4xje).
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa5007/sa5007.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call's syntax, where staticcheck's report
// puts it. The original walks every source function of a package in one run, but keeps nothing from one function to
// the next: it compares each call's static callee with the function the call is in, and judges the call by the
// dominator tree of that one function. So the port judges one function at a time and finds what the original finds.
// The registry runs the port in the original's place, under its name, and the original stays only as the oracle: the
// parity tests beside this file run both over the same packages, and they differ nowhere. The one place the port could
// differ is a call with no syntax, where staticcheck's report panics and the port reports nothing; staticcheck builds
// every call in a source function from its syntax, so no such call reaches either.
package infinite_recursion

import (
	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// Listener is what the port hears of each package from the dispatcher: every function, each judged on its own.
var Listener = rule.Listener{
	FileLocal:                  true,
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{Function: Check},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA5007",
	"SA5007",
	"Flags a function that calls itself on every path to its end: staticcheck's SA5007, ported onto its "+
		"intermediate representation.",
	&Listener,
)

// message is the original's, word for word.
const message = "infinite recursive call"

// Check judges one function: each call, deferred call included, whose static callee is the function itself, from a
// block every way out of the function passes through, so no path returns or panics without recursing first.
func Check(context *rule.Context, function *intermediate_representation.Function) {
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			site, isCallSite := instruction.(intermediate_representation.CallInstruction)
			if !isCallSite || site.Common().StaticCallee() != function {
				continue
			}
			if _, isGo := site.(*intermediate_representation.Go); isGo {
				// A goroutine that starts itself grows a new stack each time, never one stack without end.
				continue
			}
			if !dominatesEveryExit(block, function) {
				continue
			}
			source := site.Source()
			if source == nil {
				// A call with no syntax has no place to report; staticcheck's report panics on one.
				continue
			}
			context.Report(analysis.Diagnostic{Pos: source.Pos(), End: source.End(), Message: message})
		}
	}
}

// dominatesEveryExit is whether every block of function with no successor, every way out of it, is reached only
// through block. A function with no such block, one that only loops, holds it of every block, as the original does.
func dominatesEveryExit(
	block *intermediate_representation.BasicBlock,
	function *intermediate_representation.Function,
) bool {
	for exit := range function.Exits() {
		if !block.Dominates(exit) {
			return false
		}
	}
	return true
}
