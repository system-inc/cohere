package main

import (
	"strings"
	"testing"
	"time"
)

// render is the whole phase line, which is what a person actually reads.
//
// Asserted whole rather than by substring, deliberately. A defect in this package's summary line
// survived a suite of substring assertions earlier tonight: every clause was present, every number
// was correct, and the sentence they composed was wrong. A fragment check cannot see that.
func render(report *pipelineReport) string {
	builder := &strings.Builder{}
	report.Write(builder)
	return builder.String()
}

// A run where all three phases ran says so, and says nothing else.
func TestACompleteRunPrintsNoWarning(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, 4*time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, 12*time.Millisecond, "")
	report.record(phaseLint, outcomeRan, 7*time.Millisecond, "")

	got := render(report)
	want := "phases: fix ran in 4ms · types ran in 12ms · lint ran in 7ms\n"
	if got != want {
		t.Fatalf("the phase line reads wrong:\n  want %q\n  got  %q", want, got)
	}
}

// A phase cut off by an upstream bail must be reported as not-run, with the reason, and must warn.
//
// This is the case the whole file exists for. A run that bailed at types and a run that linted
// cleanly both produce zero lint findings, and only this line separates them.
func TestABailNamesTheUnreachedPhasesAndWarns(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, 4*time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, 12*time.Millisecond, "")
	report.markRemainingNotReached(phaseTypes, "4 type diagnostics")

	got := render(report)
	// Unused reads as skipped rather than as not reached, because nobody asked for it here. A bail
	// takes nothing from an opt-in phase that was never requested, and saying it did would
	// manufacture a gap out of an ordinary run.
	want := "phases: fix ran in 4ms · types ran in 12ms · lint did not run (types bailed: 4 type diagnostics)" +
		" · unused skipped (not requested — this is a report, ask for it with --unused)\n" +
		"  this run did not check everything — the phases above say what was not checked\n"
	if got != want {
		t.Fatalf("the bail line reads wrong:\n  want %q\n  got  %q", want, got)
	}
}

// An opt-in phase the caller DID ask for and that a bail then cut off is a real gap, and must read
// as one.
//
// This is the other half of the distinction above, and it is the half that would be easy to lose:
// exempting unused from the coverage warning is correct when nobody asked for it and wrong when
// somebody did. Somebody who typed --unused and got no report has had something withheld.
func TestARequestedUnusedPhaseCutOffByABailIsAGap(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, 4*time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, 12*time.Millisecond, "")
	report.markRemainingNotReachedFor(phaseTypes, "4 type diagnostics", requestedPhases{phaseUnused: true})

	got := render(report)
	if !strings.Contains(got, "unused did not run (types bailed: 4 type diagnostics)") {
		t.Fatalf("a requested unused phase that was cut off did not read as cut off: %q", got)
	}
	if !warnsAboutCoverage(got) {
		t.Fatalf("a requested phase that was withheld did not warn about coverage: %q", got)
	}
}

// A bare run must not warn merely because unused was not requested, or the warning appears on every
// ordinary run and people stop reading it — which costs exactly the case it exists for.
func TestAnUnrequestedUnusedPhaseDoesNotWarn(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, time.Millisecond, "")
	report.record(phaseLint, outcomeRan, time.Millisecond, "")
	report.record(phaseUnused, outcomeSkipped, 0, "not requested")

	got := render(report)
	if warnsAboutCoverage(got) {
		t.Fatalf("an ordinary run warned about coverage because unused was not requested: %q", got)
	}
	// The absence is still stated, which is what keeps the exemption honest.
	if !strings.Contains(got, "unused skipped") {
		t.Fatalf("the phase line did not state that unused was skipped: %q", got)
	}
}

// A bail at the first phase must mark everything after it, not just the next one.
func TestABailAtFixMarksBothLaterPhases(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, time.Millisecond, "")
	report.markRemainingNotReached(phaseFix, "the rewrite did not parse")

	got := render(report)
	if !strings.Contains(got, "types did not run") || !strings.Contains(got, "lint did not run") {
		t.Fatalf("a bail at fix left a later phase unaccounted for: %q", got)
	}
	if !warnsAboutCoverage(got) {
		t.Fatalf("a bail did not warn: %q", got)
	}
}

// warnsAboutCoverage reports whether the rendered output carries the incomplete-coverage sentence.
func warnsAboutCoverage(line string) bool {
	return strings.Contains(line, "did not check everything")
}

// `--no-fix` withholds no finding, so it must not warn. This is the continuous-integration path and
// a warning that fires on every green run is one people stop reading — which costs exactly the case
// the warning exists for.
func TestNoFixDoesNotWarn(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeSkipped, 0, "--no-fix")
	report.record(phaseTypes, outcomeRan, 12*time.Millisecond, "")
	report.record(phaseLint, outcomeRan, 7*time.Millisecond, "")

	got := render(report)
	want := "phases: fix skipped (--no-fix) · types ran in 12ms · lint ran in 7ms\n"
	if got != want {
		t.Fatalf("--no-fix warned or read wrong:\n  want %q\n  got  %q", want, got)
	}
}

// A reporting phase that was turned off is a real hole in the verdict and must warn, because its
// findings are absent from the output and a reader cannot tell that from having none.
func TestSkippingAReportingPhaseWarns(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		skipped phaseName
	}{
		{"types skipped", phaseTypes},
		{"lint skipped", phaseLint},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			report := &pipelineReport{}
			for _, name := range phaseOrder {
				if name == testCase.skipped {
					report.record(name, outcomeSkipped, 0, "not requested")
					continue
				}
				report.record(name, outcomeRan, time.Millisecond, "")
			}

			if !warnsAboutCoverage(render(report)) {
				t.Fatalf("skipping %s did not warn: %q", testCase.skipped, render(report))
			}
		})
	}
}

// The warning must be able to fire and able to stay silent. A check that has only ever done one of
// those has not been shown to discriminate.
func TestTheWarningDiscriminates(t *testing.T) {
	complete := &pipelineReport{}
	for _, name := range phaseOrder {
		complete.record(name, outcomeRan, time.Millisecond, "")
	}

	incomplete := &pipelineReport{}
	incomplete.record(phaseFix, outcomeRan, time.Millisecond, "")
	incomplete.markRemainingNotReached(phaseFix, "control")

	if warnsAboutCoverage(render(complete)) {
		t.Fatalf("the warning fired on a complete run")
	}
	if !warnsAboutCoverage(render(incomplete)) {
		t.Fatalf("the warning did not fire on an incomplete run")
	}
}

// Phases print in pipeline order regardless of the order they were recorded in. The line is read as
// a sequence, so a report that listed them by completion time would misdescribe the pipeline.
func TestPhasesPrintInPipelineOrder(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseLint, outcomeRan, time.Millisecond, "")
	report.record(phaseFix, outcomeRan, time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, time.Millisecond, "")

	got := render(report)
	fixAt := strings.Index(got, "fix ran")
	typesAt := strings.Index(got, "types ran")
	lintAt := strings.Index(got, "lint ran")
	if !(fixAt < typesAt && typesAt < lintAt) {
		t.Fatalf("phases printed out of pipeline order: %q", got)
	}
}

// markRemainingNotReached must never overwrite a phase that already reported an outcome. The phase
// that bailed ran, and recording it twice would print it twice.
func TestMarkingDoesNotDuplicateAnAlreadyRecordedPhase(t *testing.T) {
	report := &pipelineReport{}
	report.record(phaseFix, outcomeRan, time.Millisecond, "")
	report.record(phaseTypes, outcomeRan, time.Millisecond, "")
	report.record(phaseLint, outcomeRan, time.Millisecond, "")
	report.markRemainingNotReached(phaseFix, "should change nothing")

	got := render(report)
	if strings.Count(got, "types") != 1 || strings.Count(got, "lint") != 1 {
		t.Fatalf("a phase was recorded twice: %q", got)
	}
	if warnsAboutCoverage(got) {
		t.Fatalf("marking after the fact turned a complete run incomplete: %q", got)
	}
}
