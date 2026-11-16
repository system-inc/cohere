package dispatch

import (
	"fmt"
	"go/ast"
	"go/token"
	"runtime"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// panicFramesShown is how many of a panic's frames a crash names, as many as the engine's own recover names for an
// analyzer's panic.
const panicFramesShown = 5

// listenerRunMethods is in the name the runtime gives every method of listenerRun, which is where a panic's frames
// stop being the check's and start being the dispatcher's.
const listenerRunMethods = ".(*listenerRun)."

// nodeCalls is in the name the runtime gives the syntax walk's method that calls each listener's Node, and its
// closures, where a panic's frames stop being the check's for a Node call.
const nodeCalls = ".(*syntaxWalk).callNodes"

// listenerRun is one rule's listener over one package: its context, what it has reported, and the crash that ended
// it, after which it is called no more on that package. Each call recovers on its own, or for the Node calls on one
// node, the walk's one recover over them recovers each (see syntaxWalk.callNodes), so one rule's panic is that rule's
// crash and every other rule's walk goes on.
type listenerRun struct {
	registered  *registeredRule
	pass        *analysis.Pass
	context     *rule.Context
	diagnostics []analysis.Diagnostic
	crash       error

	// inherited is the files the run's rule takes the host's findings on rather than hear (rule.InheritedFiles), nil
	// for a rule that inherits none, and skipping is whether the file the walk is in is one of them, when the run hears
	// none of its calls.
	inherited map[*token.File]bool
	skipping  bool

	// timing is the run's collector under `--timing`, nil otherwise, when each call is the listener's call alone, at
	// the cost of reading this field. cpu and calls are what the measured calls cost over the package, which finish
	// bills to the rule and takes out of the dispatcher's own row.
	timing *timing.Collector
	cpu    time.Duration
	calls  int64
}

// newListenerRun is registered's listener ready to hear pass's package, with a context of its own whose findings
// collect on the run, judging generated files by isGenerated, and each call measured into collector when it is set.
// A fileLocal run hears only some of the package's files, a shard of a large package's, and its context's State
// panics (see rule.Listener.FileLocal). A listener marked SyntaxOnly always has a syntax-only context, on every build,
// and hears none of the inherited files, whose findings are the host's.
func newListenerRun(
	registered *registeredRule,
	pass *analysis.Pass,
	isGenerated func(string) bool,
	collector *timing.Collector,
	fileLocal bool,
	inherited map[*token.File]bool,
) *listenerRun {
	run := &listenerRun{registered: registered, pass: pass, timing: collector, inherited: inherited}
	newContext := rule.NewContext
	switch {
	case registered.listener.SyntaxOnly:
		newContext = rule.NewSyntaxOnlyContext
	case fileLocal:
		newContext = rule.NewFileLocalContext
	}
	run.context = newContext(registered.name, pass, registered.listener.Requires, &registered.analyzer.Flags,
		isGenerated, func(_ string, diagnostic analysis.Diagnostic) {
			run.diagnostics = append(run.diagnostics, diagnostic)
		})
	return run
}

// function calls the Function of the listener's walk over the engine's own intermediate representation on function.
func (run *listenerRun) function(function *intermediate_representation.Function) {
	if run.crash != nil || run.registered.listener.IntermediateRepresentation.Function == nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() { run.registered.listener.IntermediateRepresentation.Function(run.context, function) })
		return
	}
	run.registered.listener.IntermediateRepresentation.Function(run.context, function)
}

// instruction calls the Instruction of the listener's walk over the engine's own intermediate representation on
// instruction, a kind the walk lists.
func (run *listenerRun) instruction(instruction intermediate_representation.Instruction) {
	if run.crash != nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() {
			run.registered.listener.IntermediateRepresentation.Instruction(run.context, instruction)
		})
		return
	}
	run.registered.listener.IntermediateRepresentation.Instruction(run.context, instruction)
}

// functionExit calls the FunctionExit of the listener's walk over the engine's own intermediate representation on
// function, once its instructions have been heard.
func (run *listenerRun) functionExit(function *intermediate_representation.Function) {
	if run.crash != nil || run.registered.listener.IntermediateRepresentation.FunctionExit == nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() { run.registered.listener.IntermediateRepresentation.FunctionExit(run.context, function) })
		return
	}
	run.registered.listener.IntermediateRepresentation.FunctionExit(run.context, function)
}

// fileEnter calls the listener's FileEnter as the walk enters file.
func (run *listenerRun) fileEnter(file *ast.File) {
	if run.crash != nil || run.skipping || run.registered.listener.FileEnter == nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() { run.registered.listener.FileEnter(run.context, file) })
		return
	}
	run.registered.listener.FileEnter(run.context, file)
}

// fileExit calls the listener's FileExit as the walk leaves file.
func (run *listenerRun) fileExit(file *ast.File) {
	if run.crash != nil || run.skipping || run.registered.listener.FileExit == nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() { run.registered.listener.FileExit(run.context, file) })
		return
	}
	run.registered.listener.FileExit(run.context, file)
}

// packageExit calls the listener's PackageExit once the package has been walked.
func (run *listenerRun) packageExit() {
	if run.crash != nil || run.registered.listener.PackageExit == nil {
		return
	}
	defer run.recoverCrash()
	if run.timing != nil {
		run.measured(func() { run.registered.listener.PackageExit(run.context) })
		return
	}
	run.registered.listener.PackageExit(run.context)
}

// measured calls listen, one call into the listener, on a thread held for it, and adds what it cost to the run's. A
// call that panics adds nothing: the run has crashed, and its rule's crash is what the run reports.
func (run *listenerRun) measured(listen func()) {
	run.cpu += run.timing.Measure(listen)
	run.calls++
}

// recoverCrash, deferred around each call, records a panic in the listener as the rule's crash on the package,
// spelled as the engine spells an analyzer's panic, so a dispatched rule's crash reads as it did when the rule was an
// analyzer of its own.
func (run *listenerRun) recoverCrash() {
	recovered := recover()
	if recovered == nil {
		return
	}
	run.crashed(recovered)
}

// crashed records recovered, a panic in the listener being recovered, as the rule's crash on the package. It reads the
// panic's frames, so it is called from the deferred function that recovered it, while they are still on the stack.
func (run *listenerRun) crashed(recovered any) {
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
// listenerRun method, or the syntax walk's callNodes, that called the check, read as check.panicFrames reads them for
// an analyzer: it runs in the deferred recover, where the panicking frames are still on the stack below
// runtime.gopanic, and the runtime's own frames are left out, since a nil dereference passes through several. It is a
// copy rather than a call because the engine's is unexported in package check, which a rule package does not import.
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
		case strings.Contains(frame.Function, listenerRunMethods), strings.Contains(frame.Function, nodeCalls):
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
