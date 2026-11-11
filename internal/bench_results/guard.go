package bench_results

import (
	"fmt"
	"math"
)

// GuardVerdict is what the nightly speed guard says of tonight's cold run against the baseline of record.
type GuardVerdict string

const (
	// GuardVerdictPass is a quiet median within the threshold of the baseline's, either way.
	GuardVerdictPass GuardVerdict = "Pass"
	// GuardVerdictRegressed is a quiet median slower than the baseline's by more than the threshold.
	GuardVerdictRegressed GuardVerdict = "Regressed"
	// GuardVerdictFaster is a quiet median faster than the baseline's by more than the threshold, which asks
	// whether the baseline should move, since a guard against a slow baseline lets the gain be lost unseen.
	GuardVerdictFaster GuardVerdict = "Faster"
	// GuardVerdictUnmeasured is a night with no number to compare: the machine was never quiet, or the engine
	// was built from a modified tree. It is never a pass, so a run of them is seen rather than read as green.
	GuardVerdictUnmeasured GuardVerdict = "Unmeasured"
)

// GuardResult is one night's verdict and the numbers and commits it was reached from, so a regression names
// the two engine commits that bound it.
type GuardResult struct {
	Verdict               GuardVerdict `json:"verdict"`
	Project               string       `json:"project"`
	BaselineMedianSeconds float64      `json:"baselineMedianSeconds"`
	// TonightMedianSeconds and Ratio are zero on an Unmeasured night, which has no quiet median.
	TonightMedianSeconds float64 `json:"tonightMedianSeconds"`
	// Ratio is tonight's quiet median over the baseline's: above one is slower.
	Ratio                 float64 `json:"ratio"`
	BaselineCohereCommit  string  `json:"baselineCohereCommit"`
	TonightCohereCommit   string  `json:"tonightCohereCommit"`
	BaselineProjectCommit string  `json:"baselineProjectCommit"`
	TonightProjectCommit  string  `json:"tonightProjectCommit"`
	// Reason says why a night went unmeasured, or why it stands where it does when the verdict alone does not.
	Reason string `json:"reason,omitempty"`
}

// Guard compares tonight's cold quiet median with the baseline's. The baseline is the baseline of record, never last
// night's, so a creep of a few percent a night still trips once it adds up past the threshold.
//
// Records of different projects or machines are refused, since their numbers measure different work or different
// hardware, and so are a record of the whole root and one narrowed to one engine's project (Record.Engine), which
// measure different work on the same project, a host-only record and an every-build one (Record.EveryBuild), and a
// local build's record and a release binary's (Record.StandardLibraryTable). A baseline with no quiet cold number, or
// from a modified engine, is refused too: a baseline is a number someone can reproduce. A tonight with neither is
// Unmeasured, not an error, because a loaded night is the machine's doing, not the record's, and the guard must still
// say the night was lost.
func Guard(baseline Record, tonight Record, threshold float64) (GuardResult, error) {
	// A NaN threshold compares false with everything, so every night would pass; refuse it with the negatives.
	if !(threshold >= 0) {
		return GuardResult{}, fmt.Errorf("a threshold of %g is not a share of the baseline", threshold)
	}
	if baseline.Project.Name != tonight.Project.Name {
		return GuardResult{}, fmt.Errorf(
			"the baseline measured %q and tonight measured %q, and different projects are never compared",
			baseline.Project.Name,
			tonight.Project.Name,
		)
	}
	if baseline.Engine != tonight.Engine {
		return GuardResult{}, fmt.Errorf(
			"the baseline checked %s and tonight checked %s, and runs of different scope are never compared",
			checkedScope(baseline.Engine),
			checkedScope(tonight.Engine),
		)
	}
	if baseline.NoFix != tonight.NoFix {
		return GuardResult{}, fmt.Errorf(
			"the baseline's cold runs %s and tonight's %s, and a fixing run is never compared with a checking one",
			coldKind(baseline.NoFix),
			coldKind(tonight.NoFix),
		)
	}
	if baseline.EveryBuild != tonight.EveryBuild {
		return GuardResult{}, fmt.Errorf(
			"the baseline checked %s and tonight %s, and runs of different builds are never compared",
			buildsKind(baseline.EveryBuild),
			buildsKind(tonight.EveryBuild),
		)
	}
	if (baseline.StandardLibraryTable == nil) != (tonight.StandardLibraryTable == nil) {
		return GuardResult{}, fmt.Errorf(
			"the baseline's cohere %s and tonight's %s, and a local build is never compared with a release's",
			binaryKind(baseline.StandardLibraryTable),
			binaryKind(tonight.StandardLibraryTable),
		)
	}
	if baseline.Machine.Model != tonight.Machine.Model || baseline.Machine.Cores != tonight.Machine.Cores {
		return GuardResult{}, fmt.Errorf(
			"the baseline ran on a %s with %d cores and tonight on a %s with %d, and different machines are never compared",
			baseline.Machine.Model,
			baseline.Machine.Cores,
			tonight.Machine.Model,
			tonight.Machine.Cores,
		)
	}
	baselineQuiet := baseline.Modes[ModeCold].Quiet
	if baselineQuiet == nil {
		return GuardResult{}, fmt.Errorf(
			"the baseline of %s has no quiet %s run, and a baseline must be a quiet number",
			baseline.RecordedAt,
			ModeCold,
		)
	}
	if baseline.Cohere.Dirty {
		return GuardResult{}, fmt.Errorf(
			"the baseline's cohere %s was built from a modified tree, which no commit reproduces",
			baseline.Cohere.Commit,
		)
	}
	result := GuardResult{
		Project:               tonight.Project.Name,
		BaselineMedianSeconds: baselineQuiet.Median,
		BaselineCohereCommit:  baseline.Cohere.Commit,
		TonightCohereCommit:   tonight.Cohere.Commit,
		BaselineProjectCommit: baseline.Project.Commit,
		TonightProjectCommit:  tonight.Project.Commit,
	}
	if tonight.Cohere.Dirty {
		result.Verdict = GuardVerdictUnmeasured
		result.Reason = fmt.Sprintf(
			"tonight's cohere %s was built from a modified tree, which no commit reproduces",
			tonight.Cohere.Commit,
		)
		return result, nil
	}
	tonightQuiet := tonight.Modes[ModeCold].Quiet
	if tonightQuiet == nil {
		result.Verdict = GuardVerdictUnmeasured
		result.Reason = fmt.Sprintf("no %s run tonight was quiet, so there is no number to compare", ModeCold)
		return result, nil
	}
	result.TonightMedianSeconds = tonightQuiet.Median
	result.Ratio = tonightQuiet.Median / baselineQuiet.Median
	// A baseline median of zero gives an infinite or NaN ratio; Validate refuses a run without positive
	// verdict seconds, so only a record that skipped it can get here, and it is refused rather than judged.
	if math.IsInf(result.Ratio, 0) || math.IsNaN(result.Ratio) {
		return GuardResult{}, fmt.Errorf("the baseline's quiet median of %gs cannot be compared", baselineQuiet.Median)
	}
	switch {
	case result.Ratio > 1+threshold:
		result.Verdict = GuardVerdictRegressed
	case result.Ratio < 1-threshold:
		result.Verdict = GuardVerdictFaster
	default:
		result.Verdict = GuardVerdictPass
	}
	return result, nil
}

// coldKind says whether a record's cold runs fixed or only checked, for a refusal to name.
func coldKind(noFix bool) string {
	if noFix {
		return "checked without fixing"
	}
	return "fixed"
}

// buildsKind says which builds a record's runs asked to check, for a refusal to name.
func buildsKind(everyBuild bool) string {
	if everyBuild {
		return "every build the Go files name"
	}
	return "the host's build alone"
}

// binaryKind says whether a record's engine carried the standard library's table, for a refusal to name.
func binaryKind(table *StandardLibraryTable) string {
	if table == nil {
		return "carried no standard library table (a local build)"
	}
	return "carried release " + table.ReleaseStamp + "'s standard library table"
}

// checkedScope says what a record with engine checked, for a refusal to name: the whole root, or one
// engine's project alone.
func checkedScope(engine Engine) string {
	if engine == "" {
		return "the whole root"
	}
	return fmt.Sprintf("the %s project alone", engine)
}

// UnmeasuredStreak is how many of the newest verdicts, newest last, are Unmeasured in a row: the nights the
// guard has gone without a number, which a single green night ends.
func UnmeasuredStreak(verdicts []GuardVerdict) int {
	streak := 0
	for index := len(verdicts) - 1; index >= 0 && verdicts[index] == GuardVerdictUnmeasured; index-- {
		streak++
	}
	return streak
}
