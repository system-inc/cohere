package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// renderRequested renders a stream for a command line that asked for the phases requested names, the way
// runSwiftEngine does, and returns what renderRecords returns.
func renderRequested(
	test *testing.T,
	lines []string,
	requested map[phaseName]bool,
	engineExit int,
) (string, int, error) {
	test.Helper()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, swiftLanguage, "", time.Now())
	run.requested = requested
	for index, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			flushStream(test, stream)
			return out.String(), 1, &recordError{index: index, err: acceptError}
		}
	}
	exitCode, finishError := run.finish(engineExit, "exited")
	flushStream(test, stream)
	return out.String(), exitCode, finishError
}

// flagsOf states a command line's boolean flags for engineRequestedPhases.
func flagsOf(names ...string) (map[string]bool, func(string) string) {
	given := map[string]bool{}
	for _, name := range names {
		given[name] = true
	}
	return given, func(name string) string {
		if given[name] {
			return "true"
		}
		return "false"
	}
}

// editRecords rewrites the records of a fixture: drop removes a record, and edit changes one in place. Each
// record is its members as raw JSON, so an edit reads one with recordText and writes one as the JSON it holds,
// and a member no edit touches is written back as it was.
func editRecords(test *testing.T, lines []string, edit func(record map[string]json.RawMessage) (keep bool)) []string {
	test.Helper()
	edited := []string{}
	for _, line := range lines {
		record := map[string]json.RawMessage{}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatal(decodeError)
		}
		if !edit(record) {
			continue
		}
		encoded, encodeError := json.Marshal(record)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		edited = append(edited, string(encoded))
	}
	return edited
}

// recordText is a record's member read as a string, or "" when it is absent or holds another kind.
func recordText(record map[string]json.RawMessage, key string) string {
	text := ""
	// Ignored: a member absent or of another kind reads as "", which is no kind or name an edit looks for.
	_ = json.Unmarshal(record[key], &text)
	return text
}

// A command line asks for the phases the TypeScript run would run for it.
func TestSwiftRequestedPhasesReadTheFlagsAsTheTypeScriptRunDoes(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		flags []string
		want  string
	}{
		{nil, "fix types lint"},
		{[]string{"no-fix"}, "fix types lint"},
		{[]string{"lint"}, "lint"},
		{[]string{"types"}, "types"},
		{[]string{"fix"}, "fix"},
		{[]string{"fix", "lint"}, "fix lint"},
		{[]string{"unused"}, "fix types lint unused"},
		{[]string{"lint", "unused-deep"}, "lint unused"},
	} {
		requested := engineRequestedPhases(flagsOf(testCase.flags...))
		names := []string{}
		for _, name := range phaseOrder {
			if requested[name] {
				names = append(names, string(name))
			}
		}
		if got := strings.Join(names, " "); got != testCase.want {
			test.Errorf("cohere %v asks for %q, want %q", testCase.flags, got, testCase.want)
		}
	}
}

// `cohere --lint` on a Swift package: the engine skips fix and types as not requested and calls the run
// complete, which it is (swift/Contract.md, `complete`). LintOnly.jsonl is a real stream of that run.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary
func TestALintRunThatSkipsWhatItWasNotAskedForIsComplete(test *testing.T) {
	lint := engineRequestedPhases(flagsOf("lint"))

	output, exitCode, err := renderRequested(test, contractFixture(test, "LintOnly.jsonl"), lint, 1)
	if err != nil || exitCode != 1 {
		test.Fatalf("a --lint run with two findings: exit %d, err %v\n%s", exitCode, err, output)
	}
	requireLines(test, output,
		"[cohere-swift/toolchain-require-upcoming-features/upcomingFeatureMissing]",
		"phases: fix skipped (not requested) · types skipped (not requested) · lint ran",
	)

	// The same run with nothing found passes.
	clean := editRecords(test, contractFixture(test, "LintOnly.jsonl"), func(record map[string]json.RawMessage) bool {
		switch recordText(record, "kind") {
		case "finding":
			return false
		case "lint", "summary":
			record["findings"] = json.RawMessage("0")
			if recordText(record, "kind") == "summary" {
				record["exitCode"] = json.RawMessage("0")
			}
		}
		return true
	})
	output, exitCode, err = renderRequested(test, clean, lint, 0)
	if err != nil || exitCode != 0 {
		test.Fatalf("a clean --lint run: exit %d, err %v\n%s", exitCode, err, output)
	}
}

// A phase the caller asked for that comes back skipped still fails, whatever the engine calls it.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary
func TestARequestedPhaseThatComesBackSkippedStillFails(test *testing.T) {
	// Skipped as not requested when it was requested: a bare run reported as though it were --lint. The
	// engine is refused rather than believed.
	_, _, err := renderRequested(test, contractFixture(test, "LintOnly.jsonl"), engineRequestedPhases(flagsOf()), 1)
	if err == nil ||
		!strings.Contains(err.Error(), "the fix phase was asked for, and the engine skipped it as \"not requested\"") {
		test.Fatalf("a bare run whose fix came back not requested was accepted: %v", err)
	}

	// Skipped for any other reason, under --lint, while calling the run complete: the contradiction the
	// summary check exists for.
	otherSkip := func(complete bool) []string {
		return editRecords(test, contractFixture(test, "LintOnly.jsonl"), func(record map[string]json.RawMessage) bool {
			if recordText(record, "kind") == "phase" && recordText(record, "name") == "lint" {
				record["outcome"] = json.RawMessage(`"skipped"`)
				record["detail"] = json.RawMessage(`"no rule was configured"`)
			}
			if recordText(record, "kind") == "summary" {
				record["complete"] = json.RawMessage(strconv.FormatBool(complete))
			}
			return true
		})
	}
	lint := engineRequestedPhases(flagsOf("lint"))
	_, _, err = renderRequested(test, otherSkip(true), lint, 1)
	if err == nil || !strings.Contains(err.Error(), "calls the run complete") {
		test.Fatalf("a requested lint skipped for another reason, called complete, was accepted: %v", err)
	}

	// Called incomplete instead, it is believed and fails as an incomplete run.
	output, exitCode, err := renderRequested(test, otherSkip(false), lint, 1)
	if err != nil || exitCode != 1 || !strings.Contains(output, "this run did not check everything") {
		test.Fatalf("a requested lint skipped and called incomplete: exit %d, err %v\n%s", exitCode, err, output)
	}
}

// --format-only asks for the fix phase alone, as TypeScript's does, so the engine's types and lint skipped as
// not requested are no gap, and its format finding is the verdict (#nqb3mjv, c024df4b). Without it a phase
// flag would be read as asking for everything, and the format gate would fail on phases it never asked for.
func TestFormatOnlyAsksForTheFixPhaseAlone(test *testing.T) {
	test.Parallel()
	given, value := flagsOf("no-fix", "format-only")
	requested := engineRequestedPhases(given, value)
	if !requested[phaseFix] || requested[phaseTypes] || requested[phaseLint] || requested[phaseUnused] {
		test.Fatalf("--format-only requested %v, want fix alone", requested)
	}
	output, exitCode, renderRequestedError := renderRequested(
		test,
		contractFixture(test, "FormatOnly.jsonl"),
		requested,
		1,
	)
	if renderRequestedError != nil {
		test.Fatalf("the format-only stream was refused: %v\n%s", renderRequestedError, output)
	}
	if exitCode != 1 {
		test.Errorf("the format finding exited %d, want 1:\n%s", exitCode, output)
	}
	everything := engineRequestedPhases(flagsOf("no-fix"))
	if _, _, renderRequestedError := renderRequested(
		test,
		contractFixture(test, "FormatOnly.jsonl"),
		everything,
		1,
	); renderRequestedError == nil {
		test.Errorf("a stream skipping types and lint as not requested was accepted for a run that asked for them")
	}
}
