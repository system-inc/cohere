package check

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// panicFramesShown is how many of a panic's frames a crash names: enough to find the line, few enough to read
// on the one line a crash prints as.
const panicFramesShown = 5

// recoveringRunFunction ends the name the runtime gives recoveringRun.run, which is where a panic's frames stop
// being the analyzer's and start being the driver's.
const recoveringRunFunction = ".(*recoveringRun).run"

// wrapped is every analyzer whose Run this process has wrapped, so each is wrapped once however many runs there
// are and however many start at once. A second wrap would nest, recovering twice and naming the panic twice.
var wrapped = struct {
	mutex     sync.Mutex
	analyzers map[*analysis.Analyzer]bool
}{analyzers: map[*analysis.Analyzer]bool{}}

// wrapRuns validates analyzers and replaces Run, on each of them and every analyzer they require, with one that
// turns a panic into the action's error.
//
// This mutates package-level analyzers such as inspect.Analyzer, which looks wrong until you know why it has
// to. checker.Analyze has no recover and runs each action on its own goroutine, so a panic in any analyzer, on
// any package, dependencies included, ends the process. A wrapped copy cannot stand in for the original: a pass
// reads what it requires as pass.ResultOf[inspect.Analyzer], keyed by the original pointer, so a copy's result
// would be filed under a key nobody reads and every such lookup would come back nil. Wrapping in place keeps
// every pointer as it was. The wrapper only adds the recover, so anything else in the process that runs these
// analyzers sees the same results.
//
// The validation is inside the lock because it reads Run, which another run may be wrapping at that moment.
func wrapRuns(analyzers []*analysis.Analyzer) error {
	wrapped.mutex.Lock()
	defer wrapped.mutex.Unlock()

	if validateError := analysis.Validate(analyzers); validateError != nil {
		return fmt.Errorf("validating the rules' analyzers: %w", validateError)
	}
	var wrap func(analyzer *analysis.Analyzer)
	wrap = func(analyzer *analysis.Analyzer) {
		// An analyzer already wrapped had what it requires wrapped with it, since Requires is fixed at init.
		if wrapped.analyzers[analyzer] {
			return
		}
		wrapped.analyzers[analyzer] = true
		recovering := &recoveringRun{analyzer: analyzer, original: analyzer.Run}
		analyzer.Run = recovering.run
		for _, required := range analyzer.Requires {
			wrap(required)
		}
	}
	for _, analyzer := range analyzers {
		wrap(analyzer)
	}
	return nil
}

// panicFrames is the frames of the panic being recovered, innermost first, from where it was raised up to the
// wrapper. It runs in the deferred recover, where the panicking frames are still on the stack below
// runtime.gopanic; the runtime's own frames are left out, since a nil dereference passes through several.
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
		case strings.HasSuffix(frame.Function, recoveringRunFunction):
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
