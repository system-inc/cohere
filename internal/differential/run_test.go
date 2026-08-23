package differential

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A control the comparison did not report must come back missed, not detected.
//
// This is the assertion the whole runner exists to make, so it is also the one most worth proving
// can fail. A checkControls that returned Detected unconditionally would satisfy every happy-path
// test ever written against it and would silently bless a completely blind harness.
func TestAControlTheComparisonMissedIsReportedMissed(t *testing.T) {
	planted := []Control{{
		Name:         "verify-only",
		RelativePath: "control/Planted.ts",
		Rule:         "consistency-no-enum",
		ExpectedSide: SideVerify,
	}}

	// A report with no differences at all: the pipeline dropped it somewhere.
	missed := checkControls(planted, Report{}, nil, nil)
	if len(missed) != 1 {
		t.Fatalf("expected one control result, got %d", len(missed))
	}
	if missed[0].Detected {
		t.Fatal("a control absent from the differences must never be reported detected")
	}
	if !strings.Contains(missed[0].Detail, "consistency-no-enum") {
		t.Fatalf("the detail must name the rule that failed to fire, got %q", missed[0].Detail)
	}

	// The same control, this time actually reported. Without this the test above would pass against
	// a checkControls that can never detect anything.
	found := checkControls(planted, Report{Differences: []Difference{{
		Finding: Finding{File: "control/Planted.ts", Line: 3, Rule: "consistency-no-enum"},
		OnlyOn:  SideVerify,
	}}}, nil, nil)
	if !found[0].Detected {
		t.Fatalf("a control present in the differences on the expected side must be detected, got %q", found[0].Detail)
	}
}

// A control found on the wrong side is not a pass.
//
// This is the failure a naive check misses. The finding is there, the rule matches, the file
// matches — and the harness has proven the opposite of what it set out to prove, because the
// direction is the whole claim.
func TestAControlOnTheWrongSideIsNotDetected(t *testing.T) {
	planted := []Control{{
		Name:         "verify-only",
		RelativePath: "control/Planted.ts",
		Rule:         "consistency-no-enum",
		ExpectedSide: SideVerify,
	}}

	results := checkControls(planted, Report{Differences: []Difference{{
		Finding: Finding{File: "control/Planted.ts", Line: 3, Rule: "consistency-no-enum"},
		OnlyOn:  SideGate,
	}}}, nil, nil)

	if results[0].Detected {
		t.Fatal("a control that fired on the opposite side must not count as detected")
	}
	if !strings.Contains(results[0].Detail, "attributed to") {
		t.Fatalf("the detail must say which side actually saw it, got %q", results[0].Detail)
	}
}

// A missing coverage line must read as zero, so the vacuity guard refuses the run.
//
// The tempting bug here is a friendly default. Any plausible non-zero number would make an
// incomplete run look complete, which is the vacuous pass this package exists to prevent,
// reintroduced by the file that reports the evidence.
func TestAbsentCoverageReadsAsZeroRatherThanAGuess(t *testing.T) {
	filesWalked, rulesRun := verifyCoverageFrom([]string{
		"graph built in 1.642s — 9973 files in the program, 3407 of them ours",
		"  note: rule consistency-no-utils-folder listened to no files",
	})
	if filesWalked != 0 || rulesRun != 0 {
		t.Fatalf("a summary with no lint coverage line must read as 0/0, got %d files and %d rules", filesWalked, rulesRun)
	}

	// The real line, so this test cannot pass by never parsing anything.
	filesWalked, rulesRun = verifyCoverageFrom([]string{
		"lint: 1 findings — 23 rules over 3407 files, 2098302 nodes visited, in 813ms",
	})
	if filesWalked != 3407 || rulesRun != 23 {
		t.Fatalf("expected 3407 files and 23 rules from the real coverage line, got %d and %d", filesWalked, rulesRun)
	}
}

// Planting must never clobber an existing file.
//
// On a tree several authors are working in, overwriting somebody's file to run a self-test would be
// a worse defect than the one being looked for.
func TestPlantingRefusesToOverwriteAnExistingFile(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "Occupied.ts")
	if err := os.WriteFile(existing, []byte("export const mine = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cleanup, err := plantControls(RunOptions{
		Root:     root,
		Controls: []Control{{Name: "clobber", RelativePath: "Occupied.ts", Contents: "export enum Bad {}\n"}},
	})
	cleanup()

	if err == nil {
		t.Fatal("planting over an existing file must be refused")
	}
	contents, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(contents) != "export const mine = 1;\n" {
		t.Fatalf("the existing file was modified: %q", contents)
	}
}

// A planted control must exist during the run and be gone afterward.
func TestPlantedControlsAreWrittenThenRemoved(t *testing.T) {
	root := t.TempDir()
	control := Control{
		Name:         "verify-only",
		RelativePath: filepath.Join("nested", "Planted.ts"),
		Contents:     "export enum Planted { A = 'A' }\n",
		Rule:         "consistency-no-enum",
		ExpectedSide: SideVerify,
	}

	planted, cleanup, err := plantControls(RunOptions{Root: root, Controls: []Control{control}})
	if err != nil {
		t.Fatal(err)
	}
	if len(planted) != 1 {
		t.Fatalf("expected one planted control, got %d", len(planted))
	}

	absolutePath := filepath.Join(root, control.RelativePath)
	contents, err := os.ReadFile(absolutePath)
	if err != nil {
		t.Fatalf("the control was not written where the gates would lint it: %v", err)
	}
	if string(contents) != control.Contents {
		t.Fatalf("the control's contents did not survive planting: %q", contents)
	}

	cleanup()
	if _, err := os.Stat(absolutePath); !os.IsNotExist(err) {
		t.Fatal("the planted control was left behind, and the next person to run either gate would lint it")
	}
}

// A gate that could not be launched is an error, never an empty result.
//
// A process that never started produces empty stdout, which is byte-identical to a clean run.
func TestAGateThatCannotRunIsAnErrorNotSilence(t *testing.T) {
	output, err := runGate(context.Background(), GateCommand{
		Name:      "missing",
		Program:   filepath.Join(t.TempDir(), "no-such-binary"),
		Directory: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("a gate that does not exist must be an error, got output %q", output)
	}
	if !strings.Contains(err.Error(), "did not run") {
		t.Fatalf("the error must say the gate never ran, got %v", err)
	}
}

// A gate exiting non-zero is normal: that is what a gate with findings does.
func TestANonZeroExitIsNotAFailure(t *testing.T) {
	output, err := runGate(context.Background(), GateCommand{
		Name:      "finds-something",
		Program:   "/bin/sh",
		Arguments: []string{"-c", "echo 'a.ts:1:1: error nexus(consistency-no-enum): found'; exit 1"},
		Directory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("a non-zero exit from a gate with findings must not be an error: %v", err)
	}
	if !strings.Contains(output, "consistency-no-enum") {
		t.Fatalf("the gate's stdout must be returned even on a non-zero exit, got %q", output)
	}
}

// A shared control passes only when both gates reported it, and each way of failing says which.
//
// The three failures are genuinely different diagnoses and collapsing them would waste the control.
// Neither side means the plant never reached either gate, so the file was written somewhere they do
// not lint. One side means the plant landed but one gate's parse or normalization dropped it, which
// is the silent total-mismatch bug this control exists to catch.
func TestASharedControlNeedsBothGatesToReportIt(t *testing.T) {
	control := Control{
		Name:           "shared-enum",
		RelativePath:   "control/Planted.ts",
		Rule:           "consistency-no-enum",
		ExpectedShared: true,
	}
	planted := []Control{control}
	seen := []Finding{{File: "control/Planted.ts", Line: 4, Rule: "consistency-no-enum"}}

	both := checkControls(planted, Report{}, seen, seen)
	if !both[0].Detected {
		t.Fatalf("a shared control both gates reported must be detected, got %q", both[0].Detail)
	}

	neither := checkControls(planted, Report{}, nil, nil)
	if neither[0].Detected {
		t.Fatal("a shared control neither gate reported must not be detected")
	}
	if !strings.Contains(neither[0].Detail, "neither gate") {
		t.Fatalf("the detail must say neither gate saw it, got %q", neither[0].Detail)
	}

	verifyOnly := checkControls(planted, Report{}, seen, nil)
	if verifyOnly[0].Detected {
		t.Fatal("a shared control only verify reported must not be detected: one side dropped it")
	}
	if !strings.Contains(verifyOnly[0].Detail, "only verify") {
		t.Fatalf("the detail must name the side that saw it, got %q", verifyOnly[0].Detail)
	}

	gateOnly := checkControls(planted, Report{}, nil, seen)
	if !strings.Contains(gateOnly[0].Detail, "only the gate") {
		t.Fatalf("the detail must name the side that saw it, got %q", gateOnly[0].Detail)
	}
}

// A shared control must never satisfy ControlsProven.
//
// It proves the pipeline carries a finding; it says nothing about whether a one-sided finding would
// survive. Letting it count would turn the weaker proof into the stronger claim silently, which is
// the same class of defect as every other guard in this package.
func TestASharedControlDoesNotProveDirection(t *testing.T) {
	provenance := Provenance{
		VerifyFilesLinted: 3407,
		GateFilesLinted:   3407,
		VerifyRulesRun:    23,
		GateRulesRun:      181,
		ControlsRun: []ControlResult{
			{Name: "shared-enum", Rule: "consistency-no-enum", Detected: true},
		},
	}

	if provenance.ControlsProven() {
		t.Fatal("a detected shared control must not count as proving either direction")
	}
	if trustworthy, _ := provenance.Trustworthy(); trustworthy {
		t.Fatal("a run carrying only a shared control has not shown it can detect a difference, so it must not be trustworthy")
	}
}

// Cleanup must unwind the directories the plant created, and only those.
//
// Removing the control file and leaving its directory behind is still a mutation of a tree several
// people work in, and it shows up in their `git status` as an unexplained empty directory. An
// earlier run of this harness did exactly that and somebody else found the leftover.
func TestCleanupRemovesTheDirectoriesItCreated(t *testing.T) {
	root := t.TempDir()
	control := Control{
		Name:         "nested",
		RelativePath: filepath.Join("code-quality", "differential-control", "Planted.ts"),
		Contents:     "export enum Planted { A = 'A' }\n",
		Rule:         "consistency-no-enum",
	}

	_, cleanup, err := plantControls(RunOptions{Root: root, Controls: []Control{control}})
	if err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(root, "code-quality", "differential-control")
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("the plant should have created %s: %v", nested, err)
	}

	cleanup()

	// Both levels go, because the plant created both.
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatal("the directory the plant created was left behind")
	}
	if _, err := os.Stat(filepath.Join(root, "code-quality")); !os.IsNotExist(err) {
		t.Fatal("the outer directory the plant created was left behind")
	}
	// The tree root is never removed, however far the unwind walks.
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the tree root must survive cleanup: %v", err)
	}
}

// A directory that already existed, or that something else wrote into, must survive.
//
// The first case would be the harness deleting part of the tree it was asked to measure. The second
// is the one that matters on a shared worktree: if a sibling's file landed in that directory while
// the gates ran, removing it would destroy their work.
func TestCleanupLeavesDirectoriesItDidNotCreate(t *testing.T) {
	root := t.TempDir()
	preexisting := filepath.Join(root, "code-quality")
	if err := os.MkdirAll(preexisting, 0o755); err != nil {
		t.Fatal(err)
	}

	control := Control{
		Name:         "nested",
		RelativePath: filepath.Join("code-quality", "differential-control", "Planted.ts"),
		Contents:     "export enum Planted { A = 'A' }\n",
		Rule:         "consistency-no-enum",
	}
	_, cleanup, err := plantControls(RunOptions{Root: root, Controls: []Control{control}})
	if err != nil {
		t.Fatal(err)
	}

	// A sibling writes into the directory the plant created, while the gates would be running.
	sibling := filepath.Join(root, "code-quality", "differential-control", "SomebodyElse.ts")
	if err := os.WriteFile(sibling, []byte("export const theirs = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanup()

	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("a sibling's file inside the planted directory must survive cleanup: %v", err)
	}
	if _, err := os.Stat(preexisting); err != nil {
		t.Fatalf("a directory that existed before the plant must survive cleanup: %v", err)
	}
}

// A gate that broke must not read as a gate that found nothing.
//
// Both exit non-zero, which is the whole difficulty: a linter with findings and a linter that
// crashed are the same exit code, distinguished only by whether stdout carries anything. Treating
// the crash as findings would hand an empty finding list to the comparison, and an empty list on
// one side makes every finding on the other look one-sided while the report stays well-formed.
func TestAGateThatFailedWithNoOutputIsNotAnEmptyResult(t *testing.T) {
	_, err := runGate(context.Background(), GateCommand{
		Name:      "crashes",
		Program:   "/bin/sh",
		Arguments: []string{"-c", "echo 'cannot find module oxlint' >&2; exit 1"},
		Directory: t.TempDir(),
	})
	if err == nil {
		t.Fatal("a gate that exited non-zero having printed no findings must be an error, not an empty result")
	}
	if !strings.Contains(err.Error(), "failed rather than found nothing") {
		t.Fatalf("the error must say the gate broke rather than came back clean, got %v", err)
	}

	// The gate's own explanation has to reach the reader. Diagnosing this from an exit code alone
	// sends them to the harness instead of to the gate, which is where the fault actually is.
	//
	// This assertion does not prove the explicit `execution.Stderr` capture is what carried it:
	// Go's `Output` also fills `ExitError.Stderr` when `cmd.Stderr` is nil, so both spellings
	// surface the text and a mutation removing the capture stays green here. Verified by probing
	// both forms rather than assumed. The assertion is still worth keeping, because what has to
	// hold is that the text reaches the reader, not which mechanism delivered it.
	if !strings.Contains(err.Error(), "cannot find module oxlint") {
		t.Fatalf("the gate's stderr must be surfaced, got %v", err)
	}

	// The control: the same non-zero exit WITH findings on stdout is normal and must still pass.
	output, err := runGate(context.Background(), GateCommand{
		Name:      "finds-something",
		Program:   "/bin/sh",
		Arguments: []string{"-c", "echo 'a.ts:1:1: error nexus(consistency-no-enum): found'; exit 1"},
		Directory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("a gate with findings exits non-zero and must not be an error: %v", err)
	}
	if !strings.Contains(output, "consistency-no-enum") {
		t.Fatalf("the findings must survive, got %q", output)
	}
}

// A gate that could not be launched surfaces its stderr too.
func TestAGateThatCannotLaunchSurfacesWhatItSaid(t *testing.T) {
	_, err := runGate(context.Background(), GateCommand{
		Name:      "missing",
		Program:   filepath.Join(t.TempDir(), "no-such-binary"),
		Directory: t.TempDir(),
	})
	if err == nil {
		t.Fatal("a gate that does not exist must be an error")
	}
	if !strings.Contains(err.Error(), "did not run") {
		t.Fatalf("the error must say the gate never ran, got %v", err)
	}
}

// stderrTail keeps the end of a long stderr and stays silent when there was none.
func TestStderrTailReportsOnlyWhatThereIs(t *testing.T) {
	if tail := stderrTail("   \n  \n"); tail != "" {
		t.Fatalf("a quiet gate must add nothing to the error, got %q", tail)
	}

	var many []string
	for line := 1; line <= maximumStderrLines+4; line++ {
		many = append(many, fmt.Sprintf("line %d", line))
	}
	tail := stderrTail(strings.Join(many, "\n"))

	// The end is what says what happened; the beginning is usually a banner.
	if !strings.Contains(tail, fmt.Sprintf("line %d", maximumStderrLines+4)) {
		t.Fatalf("the last line must survive, got %q", tail)
	}
	if strings.Contains(tail, "line 1 ") {
		t.Fatalf("a long stderr must be trimmed from the front, got %q", tail)
	}
}
