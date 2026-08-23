package differential

import (
	"fmt"
	"strings"
)

// Provenance is what actually ran, recorded so that an empty difference list can be told apart
// from a harness that never looked.
//
// This is the whole reason the package exists rather than a shell script with a `diff` in it. The
// commissioning task put it plainly: two gates that both report zero, one because it checked and
// one because it ran on no files, produce identical output. Four vacuous probes were caught in one
// night, and every one of them produced a clean, confident, wrong answer. So the harness records
// its own population and refuses to render a verdict that its population does not support.
//
// The two guards below fail in opposite directions and a sweep needs both. FilesLinted catches a
// detector that was handed nothing to examine — the case a planted-violation control sails straight
// past, because a control file is itself a file and a harness linting only that one file would
// still flag it. ControlDetected catches a detector that cannot detect at all, which a healthy
// file count says nothing about.
//
// # What is proven today, and what is not
//
// The two directions of detection are not in the same state, and the asymmetry is a fact about the
// codebase rather than an oversight, so it is written here rather than left for a reader to infer
// from an empty ControlsRun.
//
// Gate-only detection has a natural population. Measured cold on ~/Projects/ahra at 01:10 on
// 2026-08-23, the gate reported 129 findings where verify reported 1, so a difference in that
// direction occurs without anyone planting anything.
//
// Verify-only detection has no natural population at all, and cannot be proven without planting a
// violation only verify can see. Until that has fired once, ControlsProven returns false and the
// report prints no verdict rather than a clean result — which is the correct answer to a question
// the harness has not earned, not a defect to be tuned away.
//
// The controls that exist today are unit-level: they prove Compare and the parsers surface a
// planted difference. Nothing yet runs both gates end to end against a real file, and the command
// that would drive that does not exist. So this package is proven as a library and unproven as an
// instrument, and a reader should not mistake a green test suite for the second thing.
type Provenance struct {
	// VerifyFilesLinted and GateFilesLinted are how many files each side actually walked. A zero
	// here means the run proved nothing regardless of what the diff says.
	VerifyFilesLinted int
	GateFilesLinted   int

	// VerifyRulesRun and GateRulesRun are how many rules each side had loaded.
	VerifyRulesRun int
	GateRulesRun   int

	// VerifyCommand and GateCommand are the exact invocations, so a reader can rerun them.
	VerifyCommand string
	GateCommand   string

	// ControlsRun records the planted-violation controls that were exercised this run, if any.
	// Empty means the harness's ability to detect a difference was not demonstrated, which is a
	// materially weaker claim and is reported as such.
	ControlsRun []ControlResult
}

// ControlResult is one planted violation and whether the harness saw it.
//
// A control is a file with a known defect that exactly one side can see. Running it proves the
// pipeline end to end: the gate ran, the parse worked, the comparison ran, and a real difference
// came out the other side. Without it, "no differences" is a claim about the codebase that is
// indistinguishable from a claim about a broken harness.
type ControlResult struct {
	// Name identifies the control in the report.
	Name string

	// ExpectedSide is the gate that should see this violation. A control that only verify can see
	// and a control that only the gate can see prove different halves of the pipeline, and a
	// harness that runs one direction only is half-proven — a parse bug on the unexercised side
	// would still report clean.
	ExpectedSide Side

	// Rule is the rule expected to fire.
	Rule string

	// Detected is whether the harness actually reported this as a difference found by
	// ExpectedSide.
	Detected bool

	// Detail is what the harness saw instead, when Detected is false.
	Detail string
}

// Trustworthy is whether this run's population supports drawing any conclusion from the diff.
//
// Deliberately strict, and deliberately not a judgment about the findings. A run over zero files
// is untrustworthy no matter how clean it looks, and a run whose controls did not fire is
// untrustworthy no matter how many files it walked.
func (provenance Provenance) Trustworthy() (bool, []string) {
	reasons := []string{}

	if provenance.VerifyFilesLinted == 0 {
		reasons = append(reasons, "verify linted 0 files, so its result is empty rather than clean")
	}
	if provenance.GateFilesLinted == 0 {
		reasons = append(reasons, "the gate linted 0 files, so its result is empty rather than clean")
	}
	if provenance.VerifyRulesRun == 0 {
		reasons = append(reasons, "verify ran 0 rules")
	}
	if provenance.GateRulesRun == 0 {
		reasons = append(reasons, "the gate ran 0 rules")
	}

	// A file count that is implausibly small is its own tell. The ahra tree is thousands of files;
	// a run reporting a handful walked something other than the tree, and that is the failure mode
	// where a clean answer is most convincing and most wrong.
	if provenance.VerifyFilesLinted > 0 && provenance.VerifyFilesLinted < minimumPlausibleFileCount {
		reasons = append(reasons, fmt.Sprintf(
			"verify linted only %d files, which is too few to be the tree — check the directory it was pointed at",
			provenance.VerifyFilesLinted,
		))
	}

	// No controls at all is its own failure, and it has to be named separately from a control that
	// ran and missed. Looping over an empty slice finds nothing wrong with it, so a run that never
	// planted anything would otherwise pass this guard vacuously — the exact shape of bug this
	// package exists to catch, one level up, inside the catcher. Found by a test, not by reading.
	if !provenance.ControlsProven() {
		reasons = append(reasons, "no control fired in both directions, so the harness has not been shown able to detect a difference")
	}

	for _, control := range provenance.ControlsRun {
		if !control.Detected {
			reasons = append(reasons, fmt.Sprintf(
				"control %q did not fire: %s — the harness has not been shown able to detect a difference",
				control.Name, control.Detail,
			))
		}
	}

	return len(reasons) == 0, reasons
}

// ControlsProven is whether both directions of detection were demonstrated.
//
// Reported separately from Trustworthy because they answer different questions. Trustworthy asks
// whether this run's population was real. This asks whether the harness was shown able to detect a
// difference at all — and both directions, because a harness that can only see one side's findings
// reports a clean diff for every defect on the other.
// A shared control leaves ExpectedSide empty and therefore satisfies neither direction, which is
// deliberate and load-bearing rather than incidental. A shared control proves the pipeline carries
// a finding end to end; it says nothing about whether a one-sided finding would survive, and
// letting it count here would turn the weaker proof into the stronger claim silently.
func (provenance Provenance) ControlsProven() bool {
	sawVerifyDirection := false
	sawGateDirection := false
	for _, control := range provenance.ControlsRun {
		if !control.Detected {
			return false
		}
		if control.ExpectedSide == SideVerify {
			sawVerifyDirection = true
		}
		if control.ExpectedSide == SideGate {
			sawGateDirection = true
		}
	}
	return sawVerifyDirection && sawGateDirection
}

// minimumPlausibleFileCount is the floor below which a run over this codebase is assumed to have
// been pointed at the wrong thing.
//
// A threshold rather than an exact expected count on purpose: the tree grows, and a harness that
// has to be edited every time a file lands is a harness that gets its guard commented out. The
// number only has to be high enough that a misconfigured run cannot slip under it, and low enough
// that it never fires on a real one.
const minimumPlausibleFileCount = 100

// Describe renders provenance for the report, as prose a reader can check.
func (provenance Provenance) Describe() string {
	builder := &strings.Builder{}

	fmt.Fprintf(builder, "verify: %d files, %d rules — %s\n",
		provenance.VerifyFilesLinted, provenance.VerifyRulesRun, provenance.VerifyCommand)
	fmt.Fprintf(builder, "gate:   %d files, %d rules — %s\n",
		provenance.GateFilesLinted, provenance.GateRulesRun, provenance.GateCommand)

	if len(provenance.ControlsRun) == 0 {
		fmt.Fprintf(builder, "controls: none run — this harness has NOT been shown able to detect a difference on this run\n")
		return builder.String()
	}

	for _, control := range provenance.ControlsRun {
		status := "detected"
		if !control.Detected {
			status = "MISSED: " + control.Detail
		}
		fmt.Fprintf(builder, "control %q (%s should see %s): %s\n",
			control.Name, control.ExpectedSide, control.Rule, status)
	}

	if !provenance.ControlsProven() {
		fmt.Fprintf(builder, "controls: only one direction exercised — a defect on the unexercised side would still report clean\n")
	}

	return builder.String()
}
