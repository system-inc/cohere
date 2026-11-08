// Package leaky_time_tick is staticcheck's SA1015, a call to time.Tick in a function that returns, whose ticker,
// before Go 1.23, could never be stopped or collected and so leaked, ported onto staticcheck's intermediate
// representation as a rule.FunctionCheck.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1015/sa1015.go, whose logic it keeps and
// whose message it reports word for word, from the start to the end of the call, where staticcheck's report puts it,
// through staticcheck_report.Report. The original walks every source function of a package in one run, but keeps
// nothing from one function to the next: it judges each by its standard library's version, its package, its file and
// whether it ever returns. So the port judges one function at a time and finds what the original finds, asking
// staticcheck_report.StandardLibraryVersion, staticcheck_code.IsMainLike and staticcheck_code.IsInTest where the
// original asks code.StdlibVersion, code.IsMainLike and code.IsInTest, each the port of the one it replaces, proven
// beside it. The original stays the oracle: the parity tests beside this file run both over the same packages, and
// they differ nowhere they hold.
package leaky_time_tick

import (
	"go/token"
	"go/version"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA1015",
	"Flags time.Tick in a function that returns, outside tests and commands, where its ticker leaks before Go "+
		"1.23: staticcheck's SA1015, ported onto its intermediate representation.",
	Check,
)

// message is the original's, word for word.
const message = "using time.Tick leaks the underlying ticker, consider using it only in endless functions, tests " +
	"and the main package, and use time.NewTicker here"

// Check judges one function: each call to time.Tick in it, when the function can return, its standard library is
// older than Go 1.23, and it is in neither a test nor a package that is, or likely implements, a command.
func Check(context *rule.Context, function *ir.Function) {
	// From Go 1.23 the collector frees a ticker no one references, stopped or not, so time.Tick no longer leaks.
	// A function with no position, which staticcheck synthesizes, can call no time.Tick, and the original skips it
	// before asking its version, which would panic for a function in none of the package's files.
	if function.Pos() == token.NoPos ||
		version.Compare(staticcheck_report.StandardLibraryVersion(context, function), "go1.23") >= 0 {
		return
	}
	if staticcheck_code.IsMainLike(context) || staticcheck_code.IsInTest(context, function) {
		return
	}
	// A function that never returns, a loop over the ticker that runs as long as the program does, never drops the
	// ticker, so it leaks nothing. Whether it returns is asked of the function that makes the call, for each call,
	// as the original asks it.
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, isCall := instruction.(*ir.Call)
			if !isCall || !irutil.IsCallTo(call.Common(), "time.Tick") {
				continue
			}
			if !irutil.Terminates(call.Parent()) {
				continue
			}
			staticcheck_report.Report(context, call, message)
		}
	}
}
