package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// longestRule is the longest rule name in GoTiming.jsonl, past timingNameWidth, so the name column widens to it.
const longestRule = "cohere-go/consistency-no-abbreviated-identifier"

// goTimingStream is the Go contract's timing fixture, its timing record changed by change, or as it is when change
// is nil.
func goTimingStream(test *testing.T, change func(record *contract.TimingRecord)) []string {
	test.Helper()
	lines := contractFixture(test, "GoTiming.jsonl")
	index := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, `{"kind":"timing"`) })
	if index < 0 {
		test.Fatal("GoTiming.jsonl holds no timing record")
	}
	if change == nil {
		return lines
	}
	record := goTimingRecord(test, lines[index])
	change(&record)
	lines[index] = timingRecordLine(test, record)
	return lines
}

// goTimingRecord decodes a fixture's timing line as the engine's type.
func goTimingRecord(test *testing.T, line string) contract.TimingRecord {
	test.Helper()
	var record contract.TimingRecord
	if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
		test.Fatal(decodeError)
	}
	return record
}

// timingRecordLine is a timing record as the engine writes it, its kind first.
func timingRecordLine(test *testing.T, record contract.TimingRecord) string {
	test.Helper()
	encoded, encodeError := json.Marshal(record)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return `{"kind":"timing",` + string(encoded[1:])
}

// renderGoTiming renders a Go stream under one output mode, through a run that asked for `--timing` or did not, and
// gives the output with the refusal of the record that broke the stream, or the finish's error.
func renderGoTiming(test *testing.T, mode outputMode, timingRequested bool, lines []string) (string, error) {
	test.Helper()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, goLanguage("module"), "", time.Now())
	run.timingRequested = timingRequested
	for _, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			flushStream(test, stream)
			return out.String(), acceptError
		}
	}
	_, finishError := run.finish(0, "exited 0")
	flushStream(test, stream)
	return out.String(), finishError
}

// timingSection is the timing table alone: its first line and every indented line under it.
func timingSection(test *testing.T, output string) string {
	test.Helper()
	start := strings.Index(output, "\ntiming:")
	if start < 0 {
		test.Fatalf("no timing table in:\n%s", output)
	}
	lines := strings.Split(output[start+1:], "\n")
	end := 1
	for end < len(lines) && strings.HasPrefix(lines[end], "  ") {
		end++
	}
	return strings.Join(lines[:end], "\n") + "\n"
}

// TestAGoRunPrintsItsTimingTable: a Go run under `--timing` prints the engine's timing record in the shape of
// TypeScript's table (#3apgjer), under the lint line. The unit first, then each rule most expensive first with its
// share of the rules' total, then each shared row by kind, the intermediate representation builds named apart, the
// load's phases with their counts, the rows' coverage of the process's CPU computed from the record's own numbers,
// and what measuring cost. A record whose rules arrive in another order prints the same table.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoRunPrintsItsTimingTable(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimingStream(test, nil)
	output, renderError := renderGoTiming(test, outputVerbose, true, lines)
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	lint := strings.Index(output, "lint: 0 findings")
	if lint < 0 || lint > strings.Index(output, "\ntiming:") {
		test.Errorf("the timing table is not under the lint line:\n%s", output)
	}
	table := timingSection(test, output)
	nameWidth := len(longestRule)
	requireLines(test, table,
		"timing: rule CPU, from each analyzer's own thread clock, not wall time: 5 rules, 10.0ms of rule CPU summed "+
			"across goroutines\n",
		fmt.Sprintf("  %-*s %9s %9s %9s\n", nameWidth, "rule", "cpu", "packages", "calls"),
		"shared: buildir, staticcheck's intermediate representation build, cost 8.0ms of CPU over 40 packages\n",
		"shared: build_intermediate_representation, the engine's intermediate representation build, cost 3.0ms of "+
			"CPU over 3 packages\n",
		"shared: syntax_dispatcher, a dispatcher's walk less its listeners, which are billed to their rules, cost "+
			"1.5ms of CPU over 3 packages\n",
		"shared: inspect, an analyzer several rules read, cost 0.50ms of CPU over 40 packages\n",
		"summed across the goroutines that ran it, so the sums run past the load's wall:\n",
		"    list: 2.0ms of CPU, and 150ms more in the go commands it ran, as the process's children's\n",
		"    parse: 4.0ms over 24 files\n",
		"    type-check: 12.0ms over 3 packages\n",
		"    export data: 6.0ms over 37 packages\n",
		"  measuring: 2,300 measured calls cost 0.23ms, already taken out of the rows above\n",
	)

	// Most expensive first, each with its share of the rules' total.
	wantRows := []struct {
		name, cpu string
		packages  int
		calls     int64
		share     string
	}{
		{longestRule, "6.0ms", 3, 1408, "  60.0%"},
		{"cohere-go/consistency-no-shouting", "2.5ms", 3, 640, "  25.0%"},
		{"cohere-go/consistency-no-long-line", "1.0ms", 3, 0, "  10.0%"},
		{"cohere-go/consistency-no-empty-function", "0.40ms", 3, 120, "   4.0%"},
		{"cohere-go/consistency-require-octal-prefix", "0.10ms", 3, 0, "   1.0%"},
	}
	previous := -1
	for _, want := range wantRows {
		row := fmt.Sprintf("  %-*s %9s %9d %9d%s\n", nameWidth, want.name, want.cpu, want.packages, want.calls,
			want.share)
		at := strings.Index(table, row)
		if at < 0 {
			test.Errorf("missing the row %q from:\n%s", row, table)
			continue
		}
		if at < previous {
			test.Errorf("%s is printed above a dearer rule:\n%s", want.name, table)
		}
		previous = at
	}

	// The coverage line's share, from the fixture's own numbers: rules, shared, the load's own CPU (its go commands'
	// is theirs, never this process's) and measuring, against the process's CPU.
	record := goTimingRecord(test, lines[slices.IndexFunc(lines, func(line string) bool {
		return strings.HasPrefix(line, `{"kind":"timing"`)
	})])
	var rows int64
	for _, rule := range record.Rules {
		rows += rule.CpuMicroseconds
	}
	for _, shared := range record.Shared {
		rows += shared.CpuMicroseconds
	}
	rows += record.Load.ListCpuMicroseconds + record.Load.ParseCpuMicroseconds + record.Load.TypeCheckCpuMicroseconds +
		record.Load.ExportDataCpuMicroseconds + record.MeasuringCpuMicroseconds
	requireLines(test, table, fmt.Sprintf(
		"  coverage: the rows sum to %s of the process's %s of CPU, %.1f%% (rules 10.0ms, shared 13.0ms, load 24.0ms, "+
			"measuring 0.23ms). The other %s is what no row measures: the garbage collector's threads, the runtime, "+
			"the rest of the run\n",
		formatMilliseconds(time.Duration(rows)*time.Microsecond),
		formatMilliseconds(time.Duration(record.ProcessCpuMicroseconds)*time.Microsecond),
		100*float64(rows)/float64(record.ProcessCpuMicroseconds),
		formatMilliseconds(time.Duration(record.ProcessCpuMicroseconds-rows)*time.Microsecond),
	))

	reversed, reversedError := renderGoTiming(test, outputVerbose, true,
		goTimingStream(test, func(record *contract.TimingRecord) {
			slices.Reverse(record.Rules)
			slices.Reverse(record.Shared)
		}))
	if reversedError != nil {
		test.Fatal(reversedError)
	}
	if got := timingSection(test, reversed); got != table {
		test.Errorf("a record with its rows in another order printed another table:\n%s\nwant\n%s", got, table)
	}

	// The default view prints it too, as a TypeScript run's table is printed whatever the view.
	human, humanError := renderGoTiming(test, outputHuman, true, lines)
	if humanError != nil {
		test.Fatal(humanError)
	}
	if got := timingSection(test, human); got != table {
		test.Errorf("the default view printed another table:\n%s\nwant\n%s", got, table)
	}
}

// TestAGoTimingRecordSaysItsExtraBuildsRestoredNothing: a record of a run with extra builds says, under the table's
// first line, that they restored nothing from the host's run and the rows include their work (#4y4qd3z), with the
// clock and without it, and a record of a run with none says nothing of builds.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoTimingRecordSaysItsExtraBuildsRestoredNothing(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	sentence := "  builds: 2 extra builds restored nothing from the host's run, because a timed run keeps nothing " +
		"for them, so every package of every build was checked from source and the rows include that work\n"
	for _, unavailable := range []string{"", "this platform has no per-thread CPU clock"} {
		output, renderError := renderGoTiming(test, outputHuman, true, goTimingStream(test,
			func(record *contract.TimingRecord) {
				record.ExtraBuildsFromSource = 2
				record.Unavailable = unavailable
			}))
		if renderError != nil {
			test.Fatalf("%v\n%s", renderError, output)
		}
		table := timingSection(test, output)
		requireLines(test, table, sentence)
		// Beside the heading, above the first row, so it qualifies every row a reader reads after it.
		if strings.Index(table, sentence) > strings.Index(table, "  "+longestRule) {
			test.Errorf("the sentence is printed below the rows:\n%s", table)
		}
	}

	output, renderError := renderGoTiming(test, outputHuman, true, goTimingStream(test, nil))
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	forbidLines(test, timingSection(test, output), "builds:", "restored nothing")
}

// TestAGoTimingRecordWithNoClockKeepsItsCounts: a record whose CPU could not be measured says why, leaves the CPU
// columns out rather than printing zeros, and keeps the counts, as printTimings does for TypeScript.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoTimingRecordWithNoClockKeepsItsCounts(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	output, renderError := renderGoTiming(test, outputHuman, true, goTimingStream(test,
		func(record *contract.TimingRecord) {
			record.Unavailable = "this platform has no per-thread CPU clock"
			record.ProcessCpuMicroseconds, record.MeasuredCalls, record.MeasuringCpuMicroseconds = 0, 0, 0
			record.Load = contract.LoadTiming{ParsedFiles: 24, TypeCheckedPackages: 3, ExportDataPackages: 37}
			for index := range record.Rules {
				record.Rules[index].CpuMicroseconds = 0
			}
			for index := range record.Shared {
				record.Shared[index].CpuMicroseconds = 0
			}
		}))
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	table := timingSection(test, output)
	requireLines(test, table,
		"timing: no rule CPU this run, because this platform has no per-thread CPU clock. The counts below are real; "+
			"the CPU columns are left out rather than shown as zero.\n",
		fmt.Sprintf("  %-*s %9s %9s\n", len(longestRule), "rule", "packages", "calls"),
		fmt.Sprintf("  %-*s %9d %9d\n", len(longestRule), longestRule, 3, 1408),
		"  shared: buildir, staticcheck's intermediate representation build, over 40 packages\n",
		"  load: parsed 24 files, type-checked 3 packages from source, read 37 packages from export data\n",
	)
	forbidLines(test, table, "cpu", "ms", "%", "coverage:", "measuring:")
}

// TestAGoTimingRecordWithNoRulesSaysSo: a record no rule ran under is said to have measured nothing, never printed
// as an empty table that reads as every rule being free.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoTimingRecordWithNoRulesSaysSo(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	output, renderError := renderGoTiming(test, outputHuman, true, goTimingStream(test,
		func(record *contract.TimingRecord) { record.Rules = []contract.RuleTiming{} }))
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	if table := timingSection(test, output); table != "timing: no rules ran, so there is nothing to measure\n" {
		test.Errorf("a record with no rules printed:\n%s", table)
	}
}

// TestAGoTimingRecordIsWrittenThroughUnderJson: under `--json` the record is one `timing` line, the engine's
// fields as it wrote them, before the summary, and no table.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestAGoTimingRecordIsWrittenThroughUnderJson(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimingStream(test, nil)
	output, renderError := renderGoTiming(test, outputJson, true, lines)
	if renderError != nil {
		test.Fatalf("%v\n%s", renderError, output)
	}
	written := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	timing := slices.IndexFunc(written, func(line string) bool { return strings.HasPrefix(line, `{"kind":"timing"`) })
	summary := slices.IndexFunc(written, func(line string) bool { return strings.Contains(line, `"kind":"summary"`) })
	if timing < 0 || summary < timing {
		test.Fatalf("no timing line before the summary:\n%s", output)
	}
	// Byte for byte: the fixture is written in the field order the engine encodes, which is the order written through.
	engineLine := lines[slices.IndexFunc(lines, func(line string) bool {
		return strings.HasPrefix(line, `{"kind":"timing"`)
	})]
	if written[timing] != engineLine {
		test.Errorf("the timing line is\n%s\nwant the engine's\n%s", written[timing], engineLine)
	}
	forbidLines(test, output, "timing: rule CPU")
}

// TestABrokenGoTimingRecordIsRefused: a timing record where the run did not ask for one, a second one, one after the
// inputs record, one missing a list the contract writes `[]`, and one with a shared row of a kind no label names, each
// break the stream, as every record out of its place does.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors read and set
func TestABrokenGoTimingRecordIsRefused(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()

	lines := goTimingStream(test, nil)
	timing := slices.IndexFunc(lines, func(line string) bool { return strings.HasPrefix(line, `{"kind":"timing"`) })
	inputs := `{"kind":"inputs","files":["/project/go.mod"],"directories":["/project"],"absent":[]}`
	for _, testCase := range []struct {
		name            string
		timingRequested bool
		lines           []string
		refusal         string
	}{
		{"the control: the record where it belongs, in a run that asked", true, lines, ""},
		{"in a run that did not ask", false, lines, "a timing record in a run that did not ask for one with --timing"},
		{
			"missing, in a run that asked",
			true,
			slices.Delete(slices.Clone(lines), timing, timing+1),
			"a summary with no timing record, in a run that asked for one with --timing",
		},
		{"twice", true, slices.Insert(slices.Clone(lines), timing, lines[timing]), "a second timing record"},
		{
			"after the inputs record",
			true,
			slices.Insert(slices.Delete(slices.Clone(lines), timing, timing+1), len(lines)-2, inputs, lines[timing]),
			"a timing record after the inputs record",
		},
		{
			"with a shared row of an unknown kind",
			true,
			goTimingStream(test, func(record *contract.TimingRecord) { record.Shared[0].Kind = "intermediate" }),
			`a timing record's shared row buildir of unknown kind "intermediate"`,
		},
	} {
		// Not parallel: renderGoTiming sets the package-level activeOutput and activeSummary
		test.Run(testCase.name, func(test *testing.T) {
			_, renderError := renderGoTiming(test, outputVerbose, testCase.timingRequested, testCase.lines)
			if testCase.refusal == "" {
				if renderError != nil {
					test.Fatalf("the control was refused: %v", renderError)
				}
				return
			}
			if renderError == nil || !strings.Contains(renderError.Error(), testCase.refusal) {
				test.Errorf("refused with %v, want %q", renderError, testCase.refusal)
			}
		})
	}

	// The engine writes `[]` for an empty list, so a list missing from the line is a broken record.
	withoutRules := slices.Clone(lines)
	withoutRules[timing] = strings.Replace(lines[timing], `"rules":[`, `"ruleRows":[`, 1)
	if _, renderError := renderGoTiming(test, outputVerbose, true, withoutRules); renderError == nil ||
		!strings.Contains(renderError.Error(), "missing its rules or shared list") {
		test.Errorf("a timing record with no rules list was refused with %v", renderError)
	}
}
