package bench_results

import (
	"fmt"
	"strings"
)

// runSummary is one project's `--json` summary line, as much of it as the benchmark reads.
type runSummary struct {
	Kind          string  `json:"kind"`
	SchemaVersion int     `json:"schemaVersion"`
	Label         string  `json:"label"`
	Verdict       string  `json:"verdict"`
	Seconds       float64 `json:"seconds"`
	Error         string  `json:"error"`
	Cache         struct {
		Replayed bool `json:"replayed"`
		Off      bool `json:"off"`
	} `json:"cache"`
	TypeErrors   int `json:"typeErrors"`
	Findings     int `json:"findings"`
	WouldChange  int `json:"wouldChange"`
	FilesInScope int `json:"filesInScope"`
	Cached       int `json:"cached"`
	// Builds are the builds a Go project's run checked, the host's first, which only a Go summary names.
	Builds []struct {
		Target string `json:"target"`
	} `json:"builds"`
	Phases []struct {
		Name    string `json:"name"`
		Outcome string `json:"outcome"`
		Detail  string `json:"detail"`
	} `json:"phases"`
	Gaps struct {
		ProgramFiles         int      `json:"programFiles"`
		CrashedFiles         int      `json:"crashedFiles"`
		RuleCrashes          int      `json:"ruleCrashes"`
		FormattingNotChecked bool     `json:"formattingNotChecked"`
		NothingToCheck       bool     `json:"nothingToCheck"`
		Unread               string   `json:"unread"`
		PhasesNotRun         []string `json:"phasesNotRun"`
		MissingGenerated     []string `json:"missingGenerated"`
		FixesHeld            bool     `json:"fixesHeld"`
	} `json:"gaps"`
}

// checkedLess says why this project's run did less than a developer's run of it, or "" when it did all of
// it. A benchmark of such a run times work that was never done: on cohere's root, a copy without git timed
// 0.18s cold for a Go engine that never ran and a Swift engine that read no input (#7z52pqf). And a missing
// generated file holds every fix and runs no rule: ahra's copy lacked the route types Next's dev server
// writes into a checkout, so its cold runs type-checked with 161 errors and never linted.
//
// A failed project that reports nothing it failed on is refused too, whether or not it says why, so a
// refusal that forgets to name its reason in a field is never timed as a run.
//
// A phase the run never reached is refused too, though no gap names it: when type-checking finds an error,
// lint is "not reached", and the run is timed without its rules (#r1kf434: ahra's October tree on today's
// dependencies read 2.4s with 2 type errors and no rule run, and the reader took it).
//
// What a developer's run meets too is no refusal: rules no file offered work, nested repositories' files,
// Swift no target compiles, Go only another platform builds, and a dependency's cgo. A modified build is the
// record's to refuse.
func (summary runSummary) checkedLess() string {
	gaps := summary.Gaps
	switch {
	case summary.Error != "":
		return summary.Error
	case len(gaps.MissingGenerated) > 0:
		return fmt.Sprintf("generated files it asks for are missing (%s)", strings.Join(gaps.MissingGenerated, ", "))
	case gaps.FixesHeld:
		return "its fixes were held, so no rule ran"
	case len(gaps.PhasesNotRun) > 0:
		return fmt.Sprintf("its run has no record of %s", strings.Join(gaps.PhasesNotRun, ", "))
	case gaps.CrashedFiles > 0 || gaps.RuleCrashes > 0:
		return fmt.Sprintf("%d files and %d rules crashed", gaps.CrashedFiles, gaps.RuleCrashes)
	case gaps.NothingToCheck:
		return "it had no files to check"
	case gaps.Unread != "":
		return "it could not check " + gaps.Unread
	case gaps.ProgramFiles > 0:
		return fmt.Sprintf("the run was narrowed from a program of %d files", gaps.ProgramFiles)
	case gaps.FormattingNotChecked:
		return "its formatting was not checked"
	case summary.phaseNotReached() != "":
		return summary.phaseNotReached()
	case summary.Verdict == "fail" && summary.Findings+summary.TypeErrors+summary.WouldChange == 0:
		return "it failed with nothing reported"
	}
	return ""
}

// phaseNotReached says which phase the run never reached and why, or "" when it reached every one.
func (summary runSummary) phaseNotReached() string {
	for _, phase := range summary.Phases {
		if phase.Outcome == "not reached" {
			return fmt.Sprintf("its %s phase was not reached (%s)", phase.Name, phase.Detail)
		}
	}
	return ""
}
