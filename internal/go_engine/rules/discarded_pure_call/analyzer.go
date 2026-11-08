// Package discarded_pure_call is staticcheck's SA4017, a call to a function without side effects whose results are
// all discarded, so the call does nothing, ported onto staticcheck's intermediate representation with the purity
// staticcheck's purity pass judges, reporting through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4017/sa4017.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call's syntax, through
// staticcheck_report.Report where the original reports through report.Report, and asks staticcheck_code.IsInTest where
// the original asks code.IsInTest. The original walks every source function of a package in one run, but keeps
// nothing from one function to the next: each call is judged by its own callee and readers. So Check judges one
// function at a time and finds what the original finds.
//
// It reads which functions have no side effects from staticcheck's purity pass, honnef.co/go/tools/analysis/facts/
// purity, whose facts cross from every dependency, straight from the pass as SA1019's port reads the deprecated
// facts. That pass is borrowed in this wave, and is to be ported with the other fact passes later. It is a plain
// analyzer and not a staticcheck_ir.FunctionCheckAnalyzer only because rule.Context does not carry that result: run
// builds the context with staticcheck_report.PassContext and the purity judgment from the pass, and hands both to
// Check for every function. When the engine's dispatcher gives the context the results of the passes a check
// requires, Check reads the purity judgment there and this shape goes.
//
// Two of the original's judgments look odd and are kept. The benchmark exception spares a function of a test file
// taking a *testing.B, but not a closure inside one, which is a function of its own taking none, so a discarded pure
// call in a benchmark's closure is reported (parity's BenchmarkClosure shows it). And a test file is known by the
// name it was parsed from, never one a line directive gives, so a function taking a *testing.B under a directive
// naming a test file is still judged, and its finding is shown at the directive's name (parity's directive.go).
//
// The original stays the oracle: the parity tests beside this file run both over the same packages, and the two differ
// nowhere they hold.
package discarded_pure_call

import (
	"fmt"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/facts/purity"
	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// buildir pass for the functions, its purity pass for which of them have no side effects, and its generated pass,
// which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA4017",
	Doc: "Flags a call to a function without side effects whose results are discarded: staticcheck's SA4017, " +
		"ported onto its intermediate representation.",
	Requires: []*analysis.Analyzer{staticcheck_ir.Analyzer, purity.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and the purity judgment, and judges every source function of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	pure, isPurity := pass.ResultOf[purity.Analyzer].(purity.Result)
	if !isPurity {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("discarded_pure_call: the purity pass's result is missing")
	}
	isPure := func(function *types.Func) bool {
		_, isHeld := pure[function]
		return isHeld
	}
	functions, readError := staticcheck_ir.Functions(pass)
	if readError != nil {
		return nil, readError
	}
	for _, function := range functions {
		Check(context, function, isPure)
	}
	return nil, nil
}

// Check judges one function: each call whose results nothing reads, of a named function isPure judges free of side
// effects.
func Check(context *rule.Context, function *ir.Function, isPure func(function *types.Func) bool) {
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
		for _, instruction := range block.Instrs {
			call, isCall := instruction.(*ir.Call)
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
