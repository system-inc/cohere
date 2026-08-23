package differential

import (
	"context"
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
	missed := checkControls(planted, Report{})
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
	}}})
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
	}}})

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
