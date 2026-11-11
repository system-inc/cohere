package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A root's one line sums its projects' counts, and names every project's gap with the project, so a combined
// green never hides a project that checked nothing (#d2xga9h).
func TestTheCombinedFooterSumsProjectsAndNamesEachGap(test *testing.T) {
	test.Parallel()
	typeScript := &projectSummary{Summary: runSummary{Rules: 428, FilesInScope: 4_900, FilesCached: 4_900}}
	swift := &projectSummary{Summary: runSummary{Rules: 57, FilesInScope: 4, FilesChecked: 4}}
	total := 100 * time.Millisecond

	clean := combinedFooter([]projectRun{
		{label: ".", summary: typeScript},
		{label: ". (Swift)", summary: swift},
	}, overallFacts{}, total, textStyle{})
	if want := "✓ 💎 0.1s (485 rules • 4 checked • 4,900 cached)"; clean != want {
		test.Errorf("two clean projects read %q, want %q", clean, want)
	}

	// The Swift project's lint never ran: the line stays green, as one project's own would, and says so with its name.
	notReached := &projectSummary{Summary: runSummary{
		Rules:        57,
		FilesInScope: 4,
		FilesChecked: 4,
		Phases:       []phaseRecord{{Name: phaseLint, Outcome: outcomeNotReached}},
	}}
	gap := combinedFooter([]projectRun{
		{label: ".", summary: typeScript},
		{label: "CapturePhone", summary: notReached},
	}, overallFacts{}, total, textStyle{})
	if !strings.HasPrefix(gap, "✓ 💎 ") || !strings.HasSuffix(gap, " • ⚠ 👑 lint did not run (CapturePhone)") {
		test.Errorf("a project whose lint did not run is not named on the line: %q", gap)
	}

	// A finding in one project fails the line, with the counts summed.
	finding := &projectSummary{Summary: runSummary{Rules: 57, Findings: 2, FilesInScope: 4, FilesChecked: 4}}
	failed := combinedFooter([]projectRun{
		{label: ".", summary: typeScript},
		{label: "swift", summary: finding, exitCode: 1},
	}, overallFacts{}, total, textStyle{})
	if want := "✗ ☠️ 0.1s • 2 findings (485 rules • 4 checked • 4,900 cached)"; failed != want {
		test.Errorf("a project with findings reads %q, want %q", failed, want)
	}

	// Would-change counts sum with their scopes, the nested part apart, so the line sets each against its own
	// (#hb90yfp).
	drift := &projectSummary{Summary: runSummary{
		Rules: 57, FilesInScope: 4, FilesChecked: 4, WouldChange: 3, NestedWouldChange: 1, WouldChangeConsidered: 6,
		NestedWouldChangeConsidered: 10,
	}}
	nestedDrift := &projectSummary{Summary: runSummary{
		Rules: 428, FilesInScope: 4_900, FilesCached: 4_900, WouldChange: 2, NestedWouldChange: 1,
		WouldChangeConsidered: 9, NestedWouldChangeConsidered: 20,
	}}
	drifted := combinedFooter([]projectRun{
		{label: ".", summary: nestedDrift, exitCode: 1},
		{label: "swift", summary: drift, exitCode: 1},
	}, overallFacts{}, total, textStyle{})
	if want := "✗ ☠️ 0.1s • 3 of 15 files would change, 2 of 30 more in nested repositories " +
		"(485 rules • 4 checked • 4,900 cached)"; drifted != want {
		test.Errorf("two projects with drift read %q, want %q", drifted, want)
	}

	// A project that did not finish, one that failed before it could hand back a summary, and one that handed none
	// back and exited zero are each named, the first two failing the line.
	missing := combinedFooter([]projectRun{
		{label: ".", summary: typeScript},
		{label: "unfinished", runError: errors.New("signal: killed"), exitCode: 1},
		{label: "refused", exitCode: 2},
		{label: "silent"},
	}, overallFacts{}, total, textStyle{})
	for _, want := range []string{
		"✗ ☠️ ",
		"⚠ did not finish (unfinished)",
		"☠️ exit 2 (refused)",
		"⚠ no summary (silent)",
	} {
		if !strings.Contains(missing, want) {
			test.Errorf("the line lacks %q: %q", want, missing)
		}
	}
}
