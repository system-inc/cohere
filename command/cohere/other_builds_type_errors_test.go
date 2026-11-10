package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// otherBuildTypeError is the compiler finding record the Go engine writes in the lint phase for a type error only
// another build found: a `_linux.go` file that fails to type-check on linux and is never compiled on the Mac.
const otherBuildTypeError = `{"kind":"finding","source":"compiler","file":"/project/broken_linux.go","line":3,` +
	`"column":9,"severity":"error","rule":"","messageId":"UndeclaredName","message":"undefined: missing",` +
	`"fixes":[],"suggestions":[]}`

// goStreamWithOtherBuildTypeError is the Go contract's clean stream as a run whose windows build found a type error
// writes it: the compiler finding record in the lint phase before the lint record, the lint record counting it in
// typeErrors beside its builds, when typeErrors is not empty, and the summary counting the finding and failing.
func goStreamWithOtherBuildTypeError(test *testing.T, typeErrors string) []string {
	test.Helper()
	var lines []string
	for _, line := range contractFixture(test, "GoClean.jsonl") {
		switch {
		case strings.HasPrefix(line, `{"kind":"lint"`):
			lines = append(lines, otherBuildTypeError)
			line = strings.TrimSuffix(line, "}") + `,"builds":` + goBuildsObject
			if typeErrors != "" {
				line += `,"typeErrors":` + typeErrors
			}
			line += "}"
		case strings.HasPrefix(line, `{"kind":"summary"`):
			line = `{"kind":"summary","findings":1,"complete":true,"exitCode":1}`
		}
		lines = append(lines, line)
	}
	return lines
}

// A type error only another build finds is a finding of the run (#rw6tg3a): the lint record counts it in typeErrors
// beside its findings, the front door accepts the record as the stream it came with, and the run reports the type
// error, counted as one, fails on it, and names the builds it checked. Before, the record counted the rule findings
// alone, and the front door refused every such run as an engine that broke its contract. A record that leaves the
// type error out of its counts is still refused, so a finding record no count holds never passes unnoticed.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestATypeErrorOnlyAnotherBuildFindsIsAFindingOfTheRun(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()

	activeOutput, activeSummary = outputSettings{Mode: outputJson}, runSummary{}
	output, exitCode, renderError := renderRecordsStartedAt(test, goLanguage("module"), engineModeCheck,
		goStreamWithOtherBuildTypeError(test, "1"), 1, nil, time.Now())
	if renderError != nil {
		test.Fatalf("the front door refused the run: %v\n%s", renderError, output)
	}
	if exitCode == 0 {
		test.Errorf("a run with a type error exited 0:\n%s", output)
	}
	if !strings.Contains(output, `"path":"/project/broken_linux.go"`) {
		test.Errorf("the type error is not among the findings:\n%s", output)
	}
	var summary struct {
		TypeErrors int               `json:"typeErrors"`
		Findings   int               `json:"findings"`
		Builds     []json.RawMessage `json:"builds"`
	}
	if decodeError := json.Unmarshal([]byte(summaryJsonOf(test, output)), &summary); decodeError != nil {
		test.Fatal(decodeError)
	}
	if summary.TypeErrors != 1 || len(summary.Builds) != 2 {
		test.Errorf("the summary counts %d type errors and names %d builds, want 1 and 2", summary.TypeErrors,
			len(summary.Builds))
	}

	activeOutput, activeSummary = outputSettings{Mode: outputJson}, runSummary{}
	output, _, renderError = renderRecordsStartedAt(test, goLanguage("module"), engineModeCheck,
		goStreamWithOtherBuildTypeError(test, ""), 1, nil, time.Now())
	if renderError == nil || !strings.Contains(renderError.Error(), "finding records came with it") {
		test.Errorf("a lint record that counts no type error beside its finding record was accepted: %v\n%s",
			renderError, output)
	}
}
