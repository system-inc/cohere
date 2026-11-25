package check

import (
	"fmt"

	"golang.org/x/tools/go/analysis"
)

// recoveringRun is one analyzer's Run with a recover around it.
type recoveringRun struct {
	analyzer *analysis.Analyzer
	original func(*analysis.Pass) (any, error)
}

// run calls the original Run, and a panic in it, or in anything it calls such as pass.Report refusing a
// malformed fix, becomes the error checker.Analyze records on the action.
func (recovering *recoveringRun) run(pass *analysis.Pass) (result any, runError error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		result = nil
		runError = fmt.Errorf("running analyzer %s on %s: it panicked with %v, at %s", recovering.analyzer.Name,
			pass.Pkg.Path(), recovered, panicFrames())
	}()
	return recovering.original(pass)
}
