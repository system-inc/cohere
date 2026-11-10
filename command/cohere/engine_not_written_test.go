package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// goStreamNotWritten is the Go contract's clean stream whose fix record names one file not written, reached through a
// link outside the root, and whose summary says complete as given, with the exit that follows from it.
func goStreamNotWritten(test *testing.T, complete bool) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	found := false
	for index, line := range lines {
		switch {
		case strings.HasPrefix(line, `{"kind":"fix"`):
			lines[index] = strings.TrimSuffix(line, "}") + `,"changedFiles":[],"notWritten":[` +
				`{"file":"/project/shared/sum.go","realPath":"/elsewhere/shared/sum.go"}]}`
			found = true
		case strings.HasPrefix(line, `{"kind":"summary"`) && !complete:
			lines[index] = `{"kind":"summary","findings":0,"complete":false,"exitCode":1}`
		}
	}
	if !found {
		test.Fatal("GoClean.jsonl holds no fix record")
	}
	return lines
}

// renderGoStreamExiting renders a Go stream under one output mode from a fresh summary, as renderGoStreamAs does, for
// a stream that exits with exitCode.
func renderGoStreamExiting(test *testing.T, mode outputMode, lines []string, exitCode int) (string, runSummary) {
	test.Helper()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	output, gotExitCode, renderRecordsInError := renderRecordsIn(test, goLanguage("module"), engineModeCheck, lines,
		exitCode, nil)
	if renderRecordsInError != nil || gotExitCode != exitCode {
		test.Fatalf("exit %d, err %v, want exit %d\n%s", gotExitCode, renderRecordsInError, exitCode, output)
	}
	return output, activeSummary
}

// A Go run whose fix phase did not write a file, since it is reached through a link outside the root (#hwz114g), names
// that gap: each file with its real path in the account `--verbose` prints, one marker in the footer every view ends
// with, the list in `--json`'s gaps, and an incomplete run reads as one whose records say where it fell short, never as
// one that fell short without saying where. A summary that calls such a run complete is refused, since its own fix
// record shows what it did not do.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestAGoRunNamesTheFilesItDidNotWrite(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	stream := goStreamNotWritten(test, false)
	wantGap := []notWrittenFile{{File: "/project/shared/sum.go", RealPath: "/elsewhere/shared/sum.go"}}
	const marker = " • ⚠ not written: 1 file reached through a link outside the root"

	verbose, summary := renderGoStreamExiting(test, outputVerbose, stream, 1)
	requireLines(test, verbose,
		"  not written: /project/shared/sum.go → /elsewhere/shared/sum.go\n",
		"this run did not check everything: the notes above name the files whose fixes were not written",
		marker,
	)
	forbidLines(test, verbose, "without saying where", "without a record saying where", "--verbose says where")
	if !reflect.DeepEqual(summary.Gaps.NotWritten, wantGap) {
		test.Errorf("the gap is %v, want %v", summary.Gaps.NotWritten, wantGap)
	}
	if summary.Gaps.Unread != "" {
		test.Errorf("a gap the records name also reads as unread: %q", summary.Gaps.Unread)
	}

	human, _ := renderGoStreamExiting(test, outputHuman, stream, 1)
	requireLines(test, human, marker)
	forbidLines(test, human, "sum.go", "--verbose says where")

	jsonOutput, _ := renderGoStreamExiting(test, outputJson, stream, 1)
	var decoded struct {
		Gaps struct {
			NotWritten []notWrittenFile `json:"notWritten"`
		} `json:"gaps"`
	}
	summaryLine := summaryJsonOf(test, jsonOutput)
	if decodeError := json.Unmarshal([]byte(summaryLine), &decoded); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, summaryLine)
	}
	if !reflect.DeepEqual(decoded.Gaps.NotWritten, wantGap) {
		test.Errorf("under --json the gap is %v, want %v:\n%s", decoded.Gaps.NotWritten, wantGap, summaryLine)
	}

	activeOutput, activeSummary = outputSettings{Mode: outputHuman}, runSummary{}
	output, _, refusal := renderRecordsIn(test, goLanguage("module"), engineModeCheck,
		goStreamNotWritten(test, true), 0, nil)
	if refusal == nil || !strings.Contains(refusal.Error(), "calls the run complete") {
		test.Errorf("a summary calling complete a run with a file not written was not refused: %v\n%s", refusal,
			output)
	}
}

// A clean Go run writes an empty list, and an engine from before the field writes none: neither names a gap, under
// `--json` or in the footer.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestAGoRunThatWroteEverythingNamesNoFileNotWritten(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	empty := contractFixture(test, "GoClean.jsonl")
	for index, line := range empty {
		if strings.HasPrefix(line, `{"kind":"fix"`) {
			empty[index] = strings.TrimSuffix(line, "}") + `,"changedFiles":[],"notWritten":[]}`
		}
	}
	for name, stream := range map[string][]string{
		"an empty list": empty,
		"no list":       contractFixture(test, "GoClean.jsonl"),
	} {
		human, summary := renderGoStreamExiting(test, outputHuman, stream, 0)
		forbidLines(test, human, "not written")
		if summary.Gaps.NotWritten != nil {
			test.Errorf("%s: the gap is %v, want none", name, summary.Gaps.NotWritten)
		}
		jsonOutput, _ := renderGoStreamExiting(test, outputJson, stream, 0)
		forbidLines(test, summaryJsonOf(test, jsonOutput), "notWritten")
	}
}
