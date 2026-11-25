package go_language

import (
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// speculativeLint is the lint phase's pass started beside the load, on a run whose findings cache has no store, so
// each package's rules start once the load has made it and its imports final rather than once every package is
// checked.
// It is speculative because the load's end decides whether the lint phase runs at all: a load with any diagnostic, or
// one that failed, means main never ran the rules, so the pass is abandoned and nothing it found, crashed on, learned
// or counted reaches a record or a note (runLint's two abandonSpeculation calls). Only a clean load reads it
// (speculativePass). It runs only when the run has no project store, so the one cache it can write is the system
// cache's store, the standard library's misses its save writes there, and only a pass that ends does: an abandoned
// one returns before its save (check.ErrAbandoned).
//
// A run that fixes keeps the pass while its fixes leave the load standing. The long-line wrap runs beside it and
// stops it only when it writes a file (wrapAfterLoad), before rereading what it wrote, since the pass reads the held
// bytes and is of a tree no longer on disk; the load after the write starts a pass of its own. The rename takes the
// pass as the lint over the first load it decides from (runRename), and a rename that writes lints a load of what
// it wrote after.
//
// It has two known costs. A run whose load ends with any diagnostic is slower than one that lints after the load,
// since the rules ran beside the load on every package it made final and all of that work is dropped: measured, a
// type error in the last package checked took a run from 2.1 to 2.8s of wall at 6.2s of user time to about 4.0s at
// 20.7s, the records identical. And a run with the findings cache's store keeps main's order, the lint after the
// load (lintOverlaps), so the gain is only on a run without the store, as `--no-cache` is.
type speculativeLint struct {
	// abandon is closed to start no more packages; finished is closed once the pass has returned, after which pass
	// and panicked are its outcome.
	abandon  chan struct{}
	finished chan struct{}
	pass     lintPass
	panicked any
}

// lintOverlaps is whether this run's lint phase starts beside its load: only on a run that lints without the
// findings cache's store. A run that wraps long lines or renames overlaps too, since each stops or takes the pass
// before it writes (see speculativeLint). A run judging each build's dependence never overlaps, since the host's
// graph is built from every package's syntax between the load's end and the rules' start (judgesDependence).
func (pipeline *pipeline) lintOverlaps() bool {
	return pipeline.overlaps &&
		pipeline.speculation == nil &&
		(pipeline.session == nil || !pipeline.session.HasStore()) &&
		pipeline.request.requests(contract.PhaseLint) &&
		!pipeline.request.judgesDependence()
}

// beginSpeculativeLint starts the lint pass over overlap's packages, each once the load has made it final, as the
// load's begin hook, which it calls on the pipeline's goroutine before the first type check.
func (pipeline *pipeline) beginSpeculativeLint(overlap *load.Overlap) {
	speculation := &speculativeLint{abandon: make(chan struct{}), finished: make(chan struct{})}
	pipeline.speculation = speculation
	go func() {
		defer close(speculation.finished)
		// A panic is held, not raised here: main would never have run the rules over a load with diagnostics, so
		// only a clean load's pass may end the process, on the pipeline's goroutine, as main's would.
		defer func() { speculation.panicked = recover() }()
		speculation.pass = pipeline.lintPackages(overlap.Packages, overlap.Rewritten, overlap.Withheld,
			check.Options{Ready: overlap.Ready, Abandon: speculation.abandon, Reads: overlap.Reads,
				Bodiless: overlap.Bodiless, WalkSlots: overlap.WalkSlots})
	}()
}

// abandonSpeculation stops the pass beside the load from starting any more packages and waits for those it started,
// then drops it whole, so nothing it did outlives this call: the run goes on as main's, which never ran it.
func (pipeline *pipeline) abandonSpeculation() {
	if pipeline.speculation == nil {
		return
	}
	close(pipeline.speculation.abandon)
	<-pipeline.speculation.finished
	pipeline.speculation = nil
	pipeline.watch(preludeLintAbandoned)
}

// speculativePass is the pass beside the load, once it has finished, for a clean load, whose lint phase is that
// pass. Its own time ran beside the types phase, so it counts for nothing here; the lint phase's time is what the run
// spent in it after the load.
func (pipeline *pipeline) speculativePass() lintPass {
	<-pipeline.speculation.finished
	if pipeline.speculation.panicked != nil {
		// Invariant: the pass panicked over a clean load, where main's pass, run on this goroutine, would have ended
		// the process with the same panic, so this one ends it too.
		panic(pipeline.speculation.panicked)
	}
	ran := pipeline.speculation.pass
	ran.elapsed = 0
	pipeline.speculation = nil
	return ran
}
