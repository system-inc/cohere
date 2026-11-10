package go_engine

import "github.com/system-inc/cohere/internal/go_engine/load"

// preludeStep is a step of the run's prelude a test watches (pipeline.watchPrelude).
type preludeStep string

const (
	// preludeLoadBegan is a load of the workspace beginning, told on the goroutine that loads.
	preludeLoadBegan preludeStep = "LoadBegan"
	// preludeFixBegan is runFix starting, told on the goroutine that runs it.
	preludeFixBegan preludeStep = "FixBegan"
	// preludeFixReturned is runFix returning, told on the goroutine that runs it.
	preludeFixReturned preludeStep = "FixReturned"
	// preludeLintAbandoned is the lint pass beside a load abandoned (abandonSpeculation), told on the pipeline's
	// goroutine.
	preludeLintAbandoned preludeStep = "LintAbandoned"
)

// loadsBesideFix is whether the run's first load runs beside the fix phase's gofmt rather than after it: only under
// `--no-fix`, and never in the editor's save, since there gofmt only checks, which on cohere's own root held the load
// back about 150ms of a prelude of about 600ms (#9wfsmqv). gofmt writes nothing, so the disk the load lists and reads
// is the disk it found; holds no new bytes, so described.contents, which the load and the lint pass beside it read
// (held), is never written; and resolves no root (writeRoot). What runFix does write the load never reads: the held fix
// record and gofmt's verdicts (engine.fix), the records and the counts behind the summary (write), incomplete, and
// stderr, which the load leaves to the join (loadBesideFix). It reads the findings cache's session only through its
// store, which keeps each entry in a file of its own. A run that writes keeps its load after gofmt, since the load must
// read what gofmt wrote, and the editor's save formats its buffer in memory, into the bytes the load reads.
func (engine *pipeline) loadsBesideFix() bool {
	return engine.request.switches["no-fix"] && engine.request.stdinPath == ""
}

// loadBesideFix runs gofmt's check (runFix) on a goroutine of its own and the run's first load on the pipeline's, as
// today's load runs, joins the check once the load has returned, and returns the load as loadWorkspace would have after
// gofmt. The check writes the stream and stderr alone until the join, so its records and notes come out as they always
// have, and the load's failure is said only after them.
//
// A load that failed or found a type error stops the lint pass beside it (speculativeLint) at once, before the join:
// the lint phase never reads that pass (runLint's abandonSpeculation calls), and nothing before it does either, since
// under `--no-fix` nothing wraps and runRename returns over such a load. Left to runLint, the pass would run on for as
// long as gofmt outlasts the load, starting packages main never lints, which in
// TestATestFunctionGoTestRefusesStopsTheLintBesideTheLoad is a 200,000-statement function whose build took a run under
// `-race` from 17s to past 8 minutes.
func (engine *pipeline) loadBesideFix(wraps bool, typesRead bool) (*load.Loaded, error) {
	finished := make(chan struct{})
	var panicked any
	go func() {
		defer close(finished)
		// A panic is held, not raised here, so the process ends on the pipeline's goroutine, as a gofmt run there
		// would have ended it, before any record of the load's.
		defer func() { panicked = recover() }()
		engine.runFix(wraps)
	}()
	loaded, loadError := engine.loadWorkspacePackages()
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		engine.abandonSpeculation()
	}
	<-finished
	if panicked != nil {
		// Invariant: runFix panicked, which on the pipeline's goroutine would have ended the process with the same
		// panic, so this one ends it too.
		panic(panicked)
	}
	engine.noteFailedLoad(loadError, typesRead)
	return loaded, loadError
}

// watch tells a test watching the prelude (watchPrelude) of step.
func (engine *pipeline) watch(step preludeStep) {
	if engine.watchPrelude != nil {
		engine.watchPrelude(step)
	}
}
