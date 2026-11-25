package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// A Go run whose listings did not share each Go file's header, as a binary linked by a Go release whose go/build is
// not the child's copy reads them (#tb538na), says why (#fnsk1jv): in the account `--verbose` prints, beside the
// memory line, and in the `--json` summary's headersUnshared. A run whose listings shared them says nothing, and a
// replay, which started no child, carries none.

// unsharedHeaders is the reason the child's types record gives, as contract/records_test.go's sample spells it.
const unsharedHeaders = "this binary links go/build of go1.28.0, and the copy that reads each Go file's header " +
	"is go1.27's"

// goStreamWithTypesField is the Go contract's clean stream with field added to its types record.
func goStreamWithTypesField(test *testing.T, field string) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	for index, line := range lines {
		if strings.Contains(line, `"kind":"types"`) {
			lines[index] = strings.TrimSuffix(line, "}") + "," + field + "}"
			return lines
		}
	}
	test.Fatal("GoClean.jsonl holds no types record")
	return nil
}

// Not parallel: it renders through contractRun, whose record acceptors set the package-level activeOutput and
// activeSummary.
func TestAGoRunWhoseHeadersWereNotSharedSaysWhy(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()

	reason, marshalError := json.Marshal(unsharedHeaders)
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	verbose, summary := renderGoStreamAs(test, outputVerbose,
		goStreamWithTypesField(test, `"headersUnshared":`+string(reason)))
	if !strings.Contains(verbose, "  headers: not shared, "+unsharedHeaders+"\n") {
		test.Errorf("the account does not say why the headers were not shared:\n%s", verbose)
	}
	if summary.HeadersUnshared != unsharedHeaders {
		test.Errorf("the summary's headersUnshared is %q, want %q", summary.HeadersUnshared, unsharedHeaders)
	}
	written, marshalError := json.Marshal(summaryAsJson(summary))
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	if !strings.Contains(string(written), `"headersUnshared":`+string(reason)) {
		test.Errorf("the --json summary does not carry headersUnshared:\n%s", written)
	}
	if replayed := replayedSummary(summary, 0); replayed.HeadersUnshared != "" {
		test.Errorf("a replay carries the recorded run's headersUnshared: %q", replayed.HeadersUnshared)
	}

	verbose, summary = renderGoStreamAs(test, outputVerbose, contractFixture(test, "GoClean.jsonl"))
	written, marshalError = json.Marshal(summaryAsJson(summary))
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	if strings.Contains(verbose, "headers:") || summary.HeadersUnshared != "" ||
		strings.Contains(string(written), "headersUnshared") {
		test.Errorf("a run whose headers were shared says they were not: %q\n%s\n%s", summary.HeadersUnshared,
			verbose, written)
	}
}
