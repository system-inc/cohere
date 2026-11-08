package dispatch

import (
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// panicFramesShown is how many of a panic's frames a crash names, as many as the engine's own recover names for an
// analyzer's panic.
const panicFramesShown = 5

// listenerRunMethods is in the name the runtime gives every method of listenerRun, which is where a panic's frames
// stop being the check's and start being the dispatcher's.
const listenerRunMethods = ".(*listenerRun)."

// listenerRun is one rule's listener over one package: its context, what it has reported, and the crash that ended
// it, after which it is called no more on that package. Each call recovers on its own, so one rule's panic is that
// rule's crash and every other rule's walk goes on.
type listenerRun struct {
	registered  *registeredRule
	pass        *analysis.Pass
	context     *rule.Context
	diagnostics []analysis.Diagnostic
	crash       error
}

// newListenerRun is registered's listener ready to hear pass's package, with a context of its own whose findings
// collect on the run, judging generated files by isGenerated.
func newListenerRun(registered *registeredRule, pass *analysis.Pass, isGenerated func(string) bool) *listenerRun {
	run := &listenerRun{registered: registered, pass: pass}
	run.context = rule.NewContext(registered.name, pass, registered.listener.Requires, &registered.analyzer.Flags,
		isGenerated, func(_ string, diagnostic analysis.Diagnostic) {
			run.diagnostics = append(run.diagnostics, diagnostic)
		})
	return run
}

// function calls the listener's Function on function.
func (run *listenerRun) function(function *ir.Function) {
	if run.crash != nil || run.registered.listener.Function == nil {
		return
	}
	defer run.recoverCrash()
	run.registered.listener.Function(run.context, function)
}

// instruction calls the listener's Instruction on instruction, a kind the listener lists.
func (run *listenerRun) instruction(instruction ir.Instruction) {
	if run.crash != nil {
		return
	}
	defer run.recoverCrash()
	run.registered.listener.Instruction(run.context, instruction)
}

// functionExit calls the listener's FunctionExit on function, once its instructions have been heard.
func (run *listenerRun) functionExit(function *ir.Function) {
	if run.crash != nil || run.registered.listener.FunctionExit == nil {
		return
	}
	defer run.recoverCrash()
	run.registered.listener.FunctionExit(run.context, function)
}

// packageExit calls the listener's PackageExit once the package has been walked.
func (run *listenerRun) packageExit() {
	if run.crash != nil || run.registered.listener.PackageExit == nil {
		return
	}
	defer run.recoverCrash()
	run.registered.listener.PackageExit(run.context)
}

// recoverCrash, deferred around each call, records a panic in the listener as the rule's crash on the package,
// spelled as the engine spells an analyzer's panic, so a dispatched rule's crash reads as it did when the rule was an
// analyzer of its own.
func (run *listenerRun) recoverCrash() {
	recovered := recover()
	if recovered == nil {
		return
	}
	run.crash = fmt.Errorf("running analyzer %s on %s: it panicked with %v, at %s", run.registered.analyzer.Name,
		run.pass.Pkg.Path(), recovered, panicFrames())
}

// outcome is what the listener found over the package: its findings, or its crash and none of them.
func (run *listenerRun) outcome() Outcome {
	if run.crash != nil {
		return Outcome{Crash: run.crash}
	}
	return Outcome{Diagnostics: run.diagnostics}
}

// panicFrames is the frames of the panic being recovered, innermost first, from where it was raised up to the
// listenerRun method that called the check, read as check.panicFrames reads them for an analyzer: it runs in the
// deferred recover, where the panicking frames are still on the stack below runtime.gopanic, and the runtime's own
// frames are left out, since a nil dereference passes through several. It is a copy rather than a call because the
// engine's is unexported in package check, which a rule package does not import.
func panicFrames() string {
	callers := make([]uintptr, 64)
	frames := runtime.CallersFrames(callers[:runtime.Callers(0, callers)])
	var shown []string
	pastPanic := false
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "runtime.gopanic":
			pastPanic = true
		case !pastPanic, strings.HasPrefix(frame.Function, "runtime."):
			// Above the panic, or inside the runtime.
		case strings.Contains(frame.Function, listenerRunMethods):
			more = false
		default:
			shown = append(shown, fmt.Sprintf("%s (%s:%d)", frame.Function, frame.File, frame.Line))
		}
		if !more || len(shown) == panicFramesShown {
			break
		}
	}
	if len(shown) == 0 {
		return "a frame the runtime did not name"
	}
	return strings.Join(shown, ", called from ")
}
