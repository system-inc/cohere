package main

import (
	"bufio"
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"
)

// renderDuplicatesStream feeds records through a fresh Go engine run that asked for --duplicates or did not, and
// ends it the way the engine would.
func renderDuplicatesStream(test *testing.T, lines []string, requested bool) (string, int, error) {
	test.Helper()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, goLanguage("module"), "", time.Now())
	run.duplicatesRequested = requested
	for _, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			flushStream(test, stream)
			return out.String(), 1, acceptError
		}
	}
	exitCode, finishError := run.finish(0, "exited 0")
	flushStream(test, stream)
	return out.String(), exitCode, finishError
}

// duplicatesRecord is a Go engine's duplicates record holding one group.
const duplicatesRecord = `{"kind":"duplicates","ran":true,"floor":11,"files":1,"filesIgnored":0,"functions":3,` +
	`"hashed":3,"groups":[{"digest":"01000000000000000000000000000000","size":19,"members":[` +
	`{"file":"/project/scores.go","line":6,"name":"ScoreOne","size":19,"digest":"01000000000000000000000000000000"},` +
	`{"file":"/project/scores.go","line":19,"name":"ScoreTwo","size":19,"digest":"01000000000000000000000000000000"}` +
	`]}],"withheld":0,"belowFloor":0,"refused":[],"notRead":{},"deadSuppressions":[],"elapsedMilliseconds":1,` +
	`"cache":{"unit":"packages","read":0,"hashed":1}}`

// The Clean contract fixture with the duplicates record after its unused phase record and before its summary, the
// one place the contract puts it.
func streamWithDuplicates(test *testing.T) []string {
	test.Helper()
	clean := contractFixture(test, "Clean.jsonl")
	return slices.Insert(slices.Clone(clean), len(clean)-1, duplicatesRecord)
}

// A Go run that asked for --duplicates prints the report its record carries, and the run's exit code and findings
// stay the engine's: the record is no finding.
// Not parallel: engineRun's record acceptors set the package-level activeSummary
func TestADuplicatesRecordPrintsTheReportAndMovesNoVerdict(test *testing.T) {
	output, exitCode, renderError := renderDuplicatesStream(test, streamWithDuplicates(test), true)
	if renderError != nil || exitCode != 0 {
		test.Fatalf("exit %d, %v:\n%s", exitCode, renderError, output)
	}
	if !strings.Contains(output, "duplicates: a report, not a gate") ||
		!strings.Contains(output, "/project/scores.go:19 ScoreTwo") {
		test.Errorf("the report did not print:\n%s", output)
	}
}

// Every way a duplicates record can break the contract is refused: one nobody asked for, one before the phases end,
// a second one, and a run that asked and ended without one.
// Not parallel: engineRun's record acceptors set the package-level activeSummary
func TestABrokenDuplicatesStreamIsRefused(test *testing.T) {
	clean := contractFixture(test, "Clean.jsonl")
	withRecord := streamWithDuplicates(test)
	twice := slices.Insert(slices.Clone(withRecord), len(withRecord)-1, duplicatesRecord)
	early := slices.Insert(slices.Clone(clean), 2, duplicatesRecord)
	cases := []struct {
		name      string
		lines     []string
		requested bool
		reason    string
	}{
		{"a record nobody asked for", withRecord, false, "did not ask for --duplicates"},
		{"a record before the phases end", early, true, "comes after every phase"},
		{"a second record", twice, true, "a second duplicates record"},
		{"no record where one was asked for", clean, true, "no duplicates record"},
	}
	for _, testCase := range cases {
		output, exitCode, renderError := renderDuplicatesStream(test, testCase.lines, testCase.requested)
		if renderError == nil || exitCode == 0 {
			test.Errorf("%s: exit %d with no refusal:\n%s", testCase.name, exitCode, output)
			continue
		}
		if !strings.Contains(renderError.Error(), testCase.reason) {
			test.Errorf("%s: refused with %q, which does not say %q", testCase.name, renderError, testCase.reason)
		}
	}
}

// A Swift run under --duplicates checks as it would and says the report did not run, since its engine has none, so a
// root holding Swift beside TypeScript or Go is never refused for asking.
// Not parallel: engineRun's record acceptors set the package-level activeSummary
func TestASwiftRunSaysItsDuplicatesReportDidNotRun(test *testing.T) {
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, swiftLanguage, "", time.Now())
	run.duplicatesUnanswered = "the Swift engine has no duplicates report"
	for _, line := range contractFixture(test, "Clean.jsonl") {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			test.Fatalf("a clean stream was refused: %v", acceptError)
		}
	}
	exitCode, finishError := run.finish(0, "exited 0")
	flushStream(test, stream)
	if finishError != nil || exitCode != 0 {
		test.Fatalf("exit %d, %v:\n%s", exitCode, finishError, out.String())
	}
	if !strings.Contains(out.String(), "not run: the Swift engine has no duplicates report") {
		test.Errorf("the report did not say it did not run:\n%s", out.String())
	}
}
