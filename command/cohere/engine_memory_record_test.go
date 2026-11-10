package main

import (
	"slices"
	"strings"
	"testing"
)

// A Go run that raised its heap's limit after its load says so, once, after its last phase and before its inputs
// (#5shepvt): in the account `--verbose` prints and in the `--json` summary. A run that raised nothing says nothing,
// a replay says nothing, and a memory record out of its place, a second one, or one with no list is a broken stream.

// raisedMemory is a memory record of one raise, to 9.7 GB.
const raisedMemory = `{"kind":"memory","raisedToBytes":[9663676416]}`

// goStreamWithMemory is the Go contract's clean stream with record placed just before its summary, after every phase.
func goStreamWithMemory(test *testing.T, records ...string) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	last := len(lines) - 1
	if !strings.Contains(lines[last], `"kind":"summary"`) {
		test.Fatalf("GoClean.jsonl does not end on its summary: %s", lines[last])
	}
	return slices.Concat(lines[:last], records, lines[last:])
}

// Not parallel: it renders through engineRun, whose record acceptors set the package-level activeOutput and
// activeSummary.
func TestAGoRunThatRaisedItsMemorySaysSo(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()

	verbose, summary := renderGoStreamAs(test, outputVerbose, goStreamWithMemory(test, raisedMemory))
	if !strings.Contains(verbose, "  memory: raised to 9.7 GB after the load\n") {
		test.Errorf("the account does not say the raise:\n%s", verbose)
	}
	if !slices.Equal(summary.MemoryRaisedToBytes, []int64{9663676416}) {
		test.Errorf("the summary's raises are %v, want the one to 9663676416", summary.MemoryRaisedToBytes)
	}
	if replayed := replayedSummary(summary, 0); replayed.MemoryRaisedToBytes != nil {
		test.Errorf("a replay carries the recorded run's raises: %v", replayed.MemoryRaisedToBytes)
	}

	twice := `{"kind":"memory","raisedToBytes":[9663676416,10737418240]}`
	if verbose, _ := renderGoStreamAs(test, outputVerbose, goStreamWithMemory(test, twice)); !strings.Contains(
		verbose, "raised to 9.7 GB, then 11 GB after the load") {
		test.Errorf("the account does not say both raises in order:\n%s", verbose)
	}

	none := `{"kind":"memory","raisedToBytes":[]}`
	verbose, summary = renderGoStreamAs(test, outputVerbose, goStreamWithMemory(test, none))
	if strings.Contains(verbose, "raised to") || summary.MemoryRaisedToBytes != nil {
		test.Errorf("a run that raised nothing says it raised: %v\n%s", summary.MemoryRaisedToBytes, verbose)
	}
}

// Not parallel: it renders through engineRun, whose record acceptors set the package-level activeSummary.
func TestAMemoryRecordOutOfItsPlaceIsRefused(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	inputs := `{"kind":"inputs","files":["/project/go.mod"],"directories":["/project"],"absent":[]}`
	clean := contractFixture(test, "GoClean.jsonl")
	for _, testCase := range []struct {
		name   string
		lines  []string
		refuse string
	}{
		{name: "after the inputs", lines: goStreamWithMemory(test, inputs, raisedMemory), refuse: "after the inputs"},
		{name: "a second one", lines: goStreamWithMemory(test, raisedMemory, raisedMemory), refuse: "a second memory"},
		{name: "before a phase", lines: slices.Concat(clean[:3], []string{raisedMemory}, clean[3:]),
			refuse: "before the"},
		{name: "no list", lines: goStreamWithMemory(test, `{"kind":"memory"}`), refuse: "no raisedToBytes"},
	} {
		activeOutput, activeSummary = outputSettings{Mode: outputVerbose}, runSummary{}
		_, _, renderError := renderRecordsIn(test, goLanguage("module"), engineModeCheck, testCase.lines, 0, nil)
		if renderError == nil || !strings.Contains(renderError.Error(), testCase.refuse) {
			test.Errorf("%s: the stream reads %v, want a refusal saying %q", testCase.name, renderError,
				testCase.refuse)
		}
	}
}
