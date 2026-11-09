// Package discarded_pure_call is staticcheck's SA4017, a call to a function without side effects whose results are
// all discarded, so the call does nothing, ported onto the engine's own intermediate representation with the purity
// function_purity judges, reporting through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4017/sa4017.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call's syntax, through
// staticcheck_report.Report where the original reports through report.Report, and asks staticcheck_code.IsInTest where
// the original asks code.IsInTest. The original walks every source function of a package in one run, but keeps
// nothing from one function to the next: each call is judged by its own callee and readers. So Check judges one
// function at a time and finds what the original finds.
//
// It reads which functions have no side effects from function_purity, staticcheck's purity pass ported onto the
// engine's own intermediate representation, whose facts cross from every dependency and are the original's, fact for
// fact (#w7zmvat). The listener requires it and reads its result through the context, handing Check the purity
// judgment for every function.
//
// Two of the original's judgments look odd and are kept. The benchmark exception spares a function of a test file
// taking a *testing.B, but not a closure inside one, which is a function of its own taking none, so a discarded pure
// call in a benchmark's closure is reported (parity's BenchmarkClosure shows it). And a test file is known by the
// name it was parsed from, never one a line directive gives, so a function taking a *testing.B under a directive
// naming a test file is still judged, and its finding is shown at the directive's name (parity's directive.go).
//
// The registry runs the port in the original's place, under its name, and the original stays only as the oracle: the
// parity tests beside this file run both over the same packages, and the two differ nowhere they hold.
package discarded_pure_call

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher over the engine's own intermediate
// representation: every function, each judged on its own with the purity judgment of the package. It requires
// function_purity, whose result it reads.
var Listener = rule.Listener{
	Requires: []*analysis.Analyzer{function_purity.Analyzer},
	IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(context *rule.Context, function *intermediate_representation.Function) {
			Check(context, function, purityJudgment(context))
		},
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4017",
	"SA4017",
	"Flags a call to a function without side effects whose results are discarded: staticcheck's SA4017, "+
		"ported onto the engine's own intermediate representation.",
	&Listener,
)

// purityJudgment is whether a function has no side effects, as function_purity holds it, read from the pass's
// result.
func purityJudgment(context *rule.Context) func(function *types.Func) bool {
	pure, isPurity := context.ResultOf(function_purity.Analyzer).(function_purity.Result)
	if !isPurity {
		// Invariant: the listener requires the purity pass, and the dispatcher hands over its result, of the type it
		// declares.
		panic("discarded_pure_call: the purity pass's result is missing")
	}
	return func(function *types.Func) bool {
		_, isHeld := pure[function]
		return isHeld
	}
}

// Check judges one function: each call whose results nothing reads, of a named function isPure judges free of side
// effects.
func Check(
	context *rule.Context,
	function *intermediate_representation.Function,
	isPure func(function *types.Func) bool,
) {
	if staticcheck_code.IsInTest(context, function) {
		for parameter := range function.Signature.Params().Variables() {
			if typeutil.IsPointerToTypeWithName(parameter.Type(), "testing.B") {
				// Ignore discarded pure functions in code related to benchmarks. Instead of matching BenchmarkFoo
				// functions, we match any function accepting a *testing.B. Benchmarks sometimes call generic
				// functions for doing the actual work, and checking for the parameter is a lot easier and faster than
				// analyzing call trees.
				return
			}
		}
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instructions {
			call, isCall := instruction.(*intermediate_representation.Call)
			if !isCall {
				continue
			}
			if referrers := call.Referrers(); referrers == nil || len(*referrers) > 0 {
				continue
			}
			callee := call.Common().StaticCallee()
			if callee == nil {
				continue
			}
			// An anonymous function has no object, and the original does not judge one.
			calleeFunction, isFunction := callee.Object().(*types.Func)
			if !isFunction || !isPure(calleeFunction) {
				continue
			}
			if context.Package.Path() == "fmt_test" && calleeFunction.FullName() == "fmt.Sprintf" {
				// The fmt package's own benchmarks call Sprintf for its cost alone.
				continue
			}
			staticcheck_report.Report(context, call,
				fmt.Sprintf("%s doesn't have side effects and its return value is ignored", calleeFunction.Name()))
		}
	}
}
