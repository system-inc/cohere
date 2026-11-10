package main

import (
	"reflect"
	"strings"
	"testing"
)

// A run that checked nothing fails on the fact, files of its language that no phase checked, and never on a reason
// having been counted (#08cv2mp, cohere's review of aaeae87e). Before, the failure rested on Excluded holding a
// reason, so a path that dropped every file without counting one read as a project with no file at all: ✓ beside
// a warning, exit 0. Each test plants that path: the files are there, nothing is checked, and no reason is counted.

// TestACheckThatDroppedEveryFileUncountedFails is a TypeScript program of two files whose type check examined
// neither and counted no reason for it, as a drop that forgot addExcluded would leave it. It fails, and names the
// two files as kept out for a reason cohere did not record.
// Not parallel: settleNothingChecked writes the package-level activeSummary, which it reads back.
func TestACheckThatDroppedEveryFileUncountedFails(test *testing.T) {
	graph, directory := buildNarrowFixtureGraph(test, map[string]string{
		"first.ts":  "export const first = 1;\n",
		"second.ts": "export const second = 2;\n",
	})
	activeSummary = runSummary{}
	// The type check ran and checked nothing, and nothing counted why: FilesChecked, FilesCached and
	// FilesTypesOnly stay 0, and Excluded empty.
	settleNothingChecked(graph, graph.ProjectFiles(), nil, projectLocation{Root: directory}, true, false)

	gaps := activeSummary.Gaps
	if !gaps.nothingChecked() {
		test.Fatalf("a run that checked none of its program's 2 files passes: %+v", gaps)
	}
	if gaps.SourceFiles != 2 {
		test.Errorf("the run counts %d files of its language, want the program's 2", gaps.SourceFiles)
	}
	want := []excludedReason{{Reason: unrecordedReason, Count: 2}}
	if !reflect.DeepEqual(gaps.Excluded, want) {
		test.Errorf("the run's reasons are %+v, want %+v", gaps.Excluded, want)
	}
	sentence := nothingCheckedSentence(gaps.Excluded)
	if want := "no files checked: 2 files excluded, " + unrecordedReason; sentence != want {
		test.Errorf("the footer says %q, want %q", sentence, want)
	}
}

// TestTheFactAloneFailsARunThatCheckedNothing holds the condition apart from the count that explains it: a run with
// files of its language that checked none fails with no reason recorded at all, and one with none passes beside its
// warning, as a program with no file of its own does.
func TestTheFactAloneFailsARunThatCheckedNothing(test *testing.T) {
	test.Parallel()
	if !(runGaps{NothingToCheck: true, SourceFiles: 1}).nothingChecked() {
		test.Error("a run that checked none of its 1 file, with no reason recorded, passes")
	}
	if (runGaps{NothingToCheck: true}).nothingChecked() {
		test.Error("a run with no file of its language fails")
	}
	if (runGaps{SourceFiles: 1}).nothingChecked() {
		test.Error("a run that checked its file fails as one that checked nothing")
	}
}

// TestAnEngineRunThatDroppedEveryFileUncountedFails is an engine's project record of three files in the package,
// none in scope, and no file excluded: the engine left every file out and said nothing of why. The engine's own
// summary exits 0, and the front door fails it, naming the three as unrecorded.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary.
func TestAnEngineRunThatDroppedEveryFileUncountedFails(test *testing.T) {
	records := contractFixture(test, "Clean.jsonl")
	records[1] = projectRecordAt(test, records[1], "/project", nil)
	records[1] = strings.Replace(records[1], `"filesInScope":2`, `"filesInScope":0`, 1)
	records[1] = strings.Replace(records[1], `"filesOurs":2`, `"filesOurs":0`, 1)
	if !strings.Contains(records[1], `"filesInPackage":3`) || !strings.Contains(records[1], `"filesInScope":0`) {
		test.Fatalf("the fixture's project record is not the one this test rewrites: %s", records[1])
	}
	activeSummary = runSummary{}
	output, code, renderRecordsError := renderRecords(test, engineModeCheck, records, 0)
	if renderRecordsError != nil {
		test.Fatalf("%v\n%s", renderRecordsError, output)
	}
	if code != 1 {
		test.Errorf("an engine run that checked none of its 3 files exits %d, want 1:\n%s", code, output)
	}
	want := []excludedReason{{Reason: unrecordedReason, Count: 3}}
	if !reflect.DeepEqual(activeSummary.Gaps.Excluded, want) {
		test.Errorf("the run's reasons are %+v, want %+v", activeSummary.Gaps.Excluded, want)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "no files checked: 3 files excluded, "+unrecordedReason) {
		test.Errorf("the footer does not name the 3 files as unrecorded:\n%s", footer)
	}
}
