package main

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// phaseName is one step of the pipeline, in the order it runs.
type phaseName string

const (
	phaseFix   phaseName = "fix"
	phaseTypes phaseName = "types"
	phaseLint  phaseName = "lint"
)

// phaseOrder is the pipeline, and the order is the design rather than a convenience.
//
//  1. an edit happens
//  2. fix and format   mutates; bails only if the result cannot be parsed
//  3. type check       bails here, alone and loudly
//  4. lint             only reachable once the code means something
//
// Mutation runs first so everything downstream sees the repaired tree: reporting a finding a fixer
// would have silently repaired wastes the reader's attention. Types gate lint because findings
// against wrong semantics are noise a reader has to re-read after fixing the real problem — one type
// error alone at the top beats one type error buried under a hundred style findings in a file that
// does not compile.
var phaseOrder = []phaseName{phaseFix, phaseTypes, phaseLint}

// phaseOutcome is what happened to one phase.
type phaseOutcome string

const (
	// outcomeRan is the only outcome that means the phase's findings can be trusted as complete.
	outcomeRan phaseOutcome = "ran"

	// outcomeSkipped is a phase the caller turned off — a flag, or `--no-fix`.
	outcomeSkipped phaseOutcome = "skipped"

	// outcomeNotReached is a phase that would have run and never got the chance, because an earlier
	// phase bailed.
	//
	// Distinct from skipped, and the distinction is the whole point of this file. A phase nobody
	// asked for and a phase that was cut off by a failure upstream both produce no findings, and a
	// reader who cannot tell them apart reads "no lint findings" off a run where lint never
	// executed.
	outcomeNotReached phaseOutcome = "not reached"
)

// phaseRecord is one phase's outcome, and what it cost.
type phaseRecord struct {
	Name     phaseName
	Outcome  phaseOutcome
	Elapsed  time.Duration
	Detail   string
	Findings int
}

// pipelineReport is what the whole run did, phase by phase.
//
// This exists because the failure this tool was built to eliminate is a confident verdict over work
// that was never done. A run that bailed at types and a run that linted cleanly both print no lint
// findings, and only a statement of which phases actually executed separates them. So the phase line
// prints on every run, including the successful ones, and a bail is never silent.
type pipelineReport struct {
	records []phaseRecord
}

// record notes what a phase did. Called once per phase, in order.
func (r *pipelineReport) record(name phaseName, outcome phaseOutcome, elapsed time.Duration, detail string) {
	r.records = append(r.records, phaseRecord{
		Name:    name,
		Outcome: outcome,
		Elapsed: elapsed,
		Detail:  detail,
	})
}

// markRemainingNotReached fills in every phase after the one that bailed.
//
// Called at the bail rather than left implicit, because a phase that is simply absent from the
// report is indistinguishable from one the reporter forgot. Every phase in the pipeline appears in
// every run's output, with a stated outcome.
func (r *pipelineReport) markRemainingNotReached(bailedAt phaseName, reason string) {
	reached := false
	for _, name := range phaseOrder {
		if name == bailedAt {
			reached = true
			continue
		}
		if !reached {
			continue
		}
		if r.has(name) {
			continue
		}
		r.record(name, outcomeNotReached, 0, fmt.Sprintf("%s bailed: %s", bailedAt, reason))
	}
}

func (r *pipelineReport) has(name phaseName) bool {
	for _, record := range r.records {
		if record.Name == name {
			return true
		}
	}
	return false
}

// checkedEverything reports whether every phase that reports findings actually ran.
//
// The warning this drives is about coverage of the *checks*, so it asks whether anything that could
// have found a problem was prevented from looking. That is a narrower question than "did all three
// phases run", and the difference is `--no-fix`: it deliberately mutates nothing, and a run that
// checked types and lint completely is not a partial check merely because it declined to write. A
// warning that fired there would appear on every continuous-integration run, and a warning that
// fires when nothing is wrong is one people learn to stop reading — which would cost exactly the
// case it exists for.
//
// A phase that was cut off is always a gap, including the fix phase, because a bail means the run
// stopped early rather than chose not to act.
func (r *pipelineReport) checkedEverything() bool {
	for _, record := range r.records {
		if record.Outcome == outcomeNotReached {
			return false
		}
		// A reporting phase that never ran is a hole in the verdict. Fix is exempt when it was
		// skipped on purpose: declining to mutate withholds no finding.
		if record.Outcome == outcomeSkipped && record.Name != phaseFix {
			return false
		}
	}
	return true
}

// Write prints the phase line: which phases ran, which did not, and why.
//
// It prints on every run rather than only on failures. The whole argument of this project is that a
// run which checked nothing must not be able to print like a run that checked everything and found
// it clean, and a phase summary that only appears when something went wrong reintroduces exactly
// that ambiguity for the successful case.
func (r *pipelineReport) Write(out io.Writer) {
	parts := make([]string, 0, len(r.records))
	for _, name := range phaseOrder {
		for _, record := range r.records {
			if record.Name != name {
				continue
			}
			switch record.Outcome {
			case outcomeRan:
				parts = append(parts, fmt.Sprintf("%s ran in %s", record.Name, round(record.Elapsed)))
			case outcomeSkipped:
				parts = append(parts, fmt.Sprintf("%s skipped (%s)", record.Name, record.Detail))
			case outcomeNotReached:
				parts = append(parts, fmt.Sprintf("%s did not run (%s)", record.Name, record.Detail))
			}
		}
	}

	fmt.Fprintf(out, "phases: %s\n", strings.Join(parts, " · "))

	// The explicit sentence for the case that matters most. A reader who takes only the last line
	// away from a bailed run must not take away a clean bill of health.
	//
	// It fires on a genuine gap in what was checked, not on every partial run. `--no-fix` withholds
	// no finding, and `--lint` alone is a deliberate narrowing whose own phase line already says so;
	// warning on those would put the sentence on ordinary runs until people stopped reading it,
	// which would cost exactly the case it exists for.
	if !r.checkedEverything() {
		fmt.Fprintf(out, "  this run did not check everything — the phases above say what was not checked\n")
	}
}
