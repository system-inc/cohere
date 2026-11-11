package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// contractFixture reads one of the fixtures swift/Contract.md defines, from the same files the Swift
// encoder's tests read, so neither side can drift without the other's test failing.
func contractFixture(test *testing.T, name string) []string {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join("..", "..", "swift", "Contract", name))
	if readError != nil {
		test.Fatal(readError)
	}
	lines := strings.Split(strings.TrimRight(string(contents), "\n"), "\n")
	if len(lines) < 2 {
		test.Fatalf("%s holds %d records, which cannot be a contract fixture", name, len(lines))
	}
	return lines
}

// renderRecords feeds records through a fresh renderer and ends the run the way the engine would.
func renderRecords(test *testing.T, mode engineMode, lines []string, engineExit int) (string, int, error) {
	test.Helper()
	return renderRecordsIn(test, swiftLanguage, mode, lines, engineExit, nil)
}

// renderRecordsYielding is renderRecords for a run that yields directories, resolved, to the Swift projects
// below it, as runSwiftEngine hands them over.
func renderRecordsYielding(
	test *testing.T,
	mode engineMode,
	lines []string,
	engineExit int,
	yieldedDirectories []string,
) (string, int, error) {
	test.Helper()
	return renderRecordsIn(test, swiftLanguage, mode, lines, engineExit, yieldedDirectories)
}

// renderRecordsIn is renderRecords in the words of any engine, yielding the directories given, so one
// helper renders every engine's stream the way runEngineBinaryUntil would.
func renderRecordsIn(
	test *testing.T,
	language engineLanguage,
	mode engineMode,
	lines []string,
	engineExit int,
	yieldedDirectories []string,
) (string, int, error) {
	test.Helper()
	return renderRecordsStartedAt(test, language, mode, lines, engineExit, yieldedDirectories, time.Now())
}

// renderRecordsStartedAt is renderRecordsIn for a process that started at processStart, which the account's total
// line measures from, so a test can give the render a total it did not have to spend.
func renderRecordsStartedAt(
	test *testing.T,
	language engineLanguage,
	mode engineMode,
	lines []string,
	engineExit int,
	yieldedDirectories []string,
	processStart time.Time,
) (string, int, error) {
	test.Helper()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, mode, language, "", processStart)
	run.yieldedDirectories = yieldedDirectories
	for index, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			run.writeUnfinished(language.theEngine() + " broke its contract")
			flushStream(test, stream)
			return out.String(), 1, &recordError{index: index, acceptError: acceptError}
		}
	}
	exitCode, finishError := run.finish(engineExit, "exited "+string(rune('0'+engineExit)))
	flushStream(test, stream)
	return out.String(), exitCode, finishError
}

// recordError says which record a refusal came at, so a failing case names the line that tripped.
type recordError struct {
	index       int
	acceptError error
}

func (recordError *recordError) Error() string { return recordError.acceptError.Error() }

func requireLines(test *testing.T, output string, wanted ...string) {
	test.Helper()
	for _, line := range wanted {
		if !strings.Contains(output, line) {
			test.Errorf("missing %q from:\n%s", line, output)
		}
	}
}

func forbidLines(test *testing.T, output string, unwanted ...string) {
	test.Helper()
	for _, line := range unwanted {
		if strings.Contains(output, line) {
			test.Errorf("printed %q, which it must not:\n%s", line, output)
		}
	}
}

// Not parallel: its subtests render check runs through engineRun, whose record acceptors set the package-level
// activeSummary
func TestSwiftRunRendersEachContractFixture(test *testing.T) {
	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("Clean", func(test *testing.T) {
		output, exitCode, renderRecordsError := renderRecords(
			test,
			engineModeCheck,
			contractFixture(test, "Clean.jsonl"),
			0,
		)
		if renderRecordsError != nil || exitCode != 0 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		requireLines(test, output,
			"package described in 412ms: 3 Swift files in the package, 2 of them ours\n",
			"fix: 0 of 2 files rewritten, 0 fixes applied, 0 refused\n",
			"format scope: changed files (working tree against HEAD): 0\n",
			"types: 0 diagnostics over 2 files in 820ms\n",
			"lint: 0 findings \u2014 4 rules over 2 files, 512 nodes visited, walked by the fix phase (nothing was rewritten, so its findings still hold)\n",
			"coverage: 4 rules = 4 ran and found nothing\n",
			"  0 files crashed · details: cohere --coverage\n",
			"  config: no CohereSettings.json beside Package.swift, so every house rule ran at error",
			"  not checked: /project/Sources/Example/Generated.swift (ignored by git)\n",
			"phases: fix ran in 31ms · types ran in 820ms · lint reused the fix phase's walk (nothing was rewritten) · unused skipped",
			": package 412ms",
		)
		forbidLines(test, output, "did not check everything", "modified tree", "in scope (")
	})

	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("Findings", func(test *testing.T) {
		output, exitCode, renderRecordsError := renderRecords(
			test,
			engineModeCheck,
			contractFixture(test, "Findings.jsonl"),
			1,
		)
		if renderRecordsError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		requireLines(test, output,
			"fix: 1 of 2 files rewritten, 1 fixes applied, 0 refused, 1 reformatted\n",
			"/project/Sources/Example/Warning.swift:2:9 - warning: variable 'neverMutated' was never mutated; consider changing to 'let' constant [#VariableNeverMutated]\n",
			// The message's newline is collapsed, so the rule tag stays on the line with the position.
			"/project/Sources/Example/ByteRing.swift:159:61 - A force unwrap crashes the process when the value is nil. Say what happens on nil with guard let or if let. [cohere-swift/force-unwrapping/forceUnwrap]\n",
			"coverage: 4 rules = 1 found something + 2 ran and found nothing + 1 listened to no files\n",
			"  this binary was built from a modified tree, so no commit reproduces these findings\n",
		)
		// The silent rule is counted by default and named behind --coverage, as a TypeScript run does.
		forbidLines(test, output, "did not check everything", "cohere-swift/force-cast")
	})

	// --coverage names each rule the record names exactly once, under its category, and counts the
	// ones the record only counts.
	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("FindingsWithCoverage", func(test *testing.T) {
		var out bytes.Buffer
		stream := bufio.NewWriter(&out)
		run := newEngineRun(stream, engineModeCheck, swiftLanguage, "", time.Now())
		run.details = true
		for _, line := range contractFixture(test, "Findings.jsonl") {
			if acceptError := run.accept([]byte(line)); acceptError != nil {
				test.Fatal(acceptError)
			}
		}
		flushStream(test, stream)
		output := out.String()
		requireLines(test, output,
			"  listened to no files (1): nothing gave it a file, or it declined every one",
			"    cohere-swift/force-cast\n",
			"  ran and found nothing (2): ",
			"    2 the engine's record counts and does not name\n",
		)
		if count := strings.Count(output, "cohere-swift/force-cast\n"); count != 1 {
			test.Errorf("the silent rule is named %d times, want 1:\n%s", count, output)
		}
		forbidLines(test, output, "details: cohere --coverage")
	})

	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("TypesBail", func(test *testing.T) {
		output, exitCode, renderRecordsError := renderRecords(
			test,
			engineModeCheck,
			contractFixture(test, "TypesBail.jsonl"),
			1,
		)
		if renderRecordsError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		requireLines(test, output,
			"/project/Sources/Example/Broken.swift:3:23 - error: cannot convert value of type 'String' to specified type 'Int'\n",
			"fix skipped (--no-fix)",
			"lint did not run (types bailed: 1 type errors \u2014 lint findings against wrong semantics are noise)",
			"  this run did not check everything: the phases above say what was not checked\n",
		)
		// A compiler finding with no group has no bracket.
		forbidLines(test, output, "Int' [#")
	})

	// Contract 2: the file the engine could not read is named with the excluded files, and the run is
	// incomplete with exit 1 though every phase ran and nothing was found.
	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("Unreadable", func(test *testing.T) {
		output, exitCode, renderRecordsError := renderRecords(
			test,
			engineModeCheck,
			contractFixture(test, "Unreadable.jsonl"),
			1,
		)
		if renderRecordsError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		requireLines(test, output,
			"  not checked: /project/Sources/Example/Latin1.swift (could not be read: it is not valid UTF-8 or could not be opened (Foundation could not decode it as UTF-8))\n",
			"  not checked: /project/Sources/Example/Generated.swift (ignored by git)\n",
			"  this run did not check everything: the notes above name the files nothing checked\n",
		)
	})

	// The --unused report renders after lint and is not a gate: two items, and the summary still counts no
	// findings and exits 0. Were the renderer to count them, the summary check would refuse the stream.
	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("Unused", func(test *testing.T) {
		output, exitCode, renderRecordsError := renderRecords(
			test,
			engineModeCheck,
			contractFixture(test, "Unused.jsonl"),
			0,
		)
		if renderRecordsError != nil || exitCode != 0 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		// The dashes are escaped, not changed: they are the report's bytes, in the TypeScript unused report's shape.
		requireLines(test, output,
			"\nunused: a report, not a gate \u2014 nothing here fails a build\n",
			"\n  imports nothing uses \u2014 3\n",
			// Two items on one line arrive out of column order and print in it.
			"    /project/Sources/Example/Main.swift:1:1 \u2014 import Foundation\n    /project/Sources/Example/Main.swift:1:20 \u2014 import Darwin\n",
			"    /project/Sources/Example/Tally.swift:2:1 \u2014 import Combine\n",
			"  looked at 2 files and 6 imports (cohere-swift/correctness-no-unused-import)\n",
			"  not checked for unused imports: 1 files (it has #if, and the index describes only the configuration the build compiled)\n",
			"  not checked for unused imports: 1 files (the build has not compiled it as it stands)\n",
			"  never reported: 1 imports (re-exported with @_exported, which is API)\n",
			// The second rule's section, from a real run on ahraos-presence: its own heading and count, its
			// items, and its own coverage lines, every skip reason by name.
			"\n  declarations nothing uses \u2014 2\n",
			"    /project/Sources/AhraOSPresence/Stage/StagePane+HandTouchProbe.swift:42:13 \u2014 var deepestAt\n",
			"    /project/Sources/AhraOSPresence/Studio/Panels/VariantsMenuProbe.swift:349:25 \u2014 func after(_:_:)\n",
			"  looked at 1092 files and 4311 declarations (cohere-swift/correctness-no-unused-declaration)\n",
			"  not checked for unused declarations: 156 files (it has #if, and the index describes only the configuration the build compiled)\n",
			"  never reported: 358 declarations (a stored property of a type whose conformances may read every stored property (Codable))\n",
			"  never reported: 175 declarations (an override or a protocol witness, reached through what it overrides)\n",
			"  never reported: 23 declarations (reached by the Objective-C runtime (@objc, @IBAction, @IBOutlet, @NSManaged, dynamic))\n",
		)
		forbidLines(test, output, "did not check everything", "Main.swift:1:1 - ", "deepestAt - ")
		if strings.Index(output, "imports nothing uses") < strings.Index(output, "lint: ") {
			test.Errorf("the unused report printed before the lint line:\n%s", output)
		}
		// Each rule's section in the order the engine wrote them, and every skip reason printed: ten for
		// declarations, counted rather than listed so a dropped one cannot hide among the pinned three.
		if strings.Index(output, "declarations nothing uses") < strings.Index(output, "never reported: 1 imports") {
			test.Errorf("the declarations section printed before the imports section ended:\n%s", output)
		}
		if got := strings.Count(output, "never reported: "); got != 11 {
			test.Errorf(
				"%d never-reported lines, the fixture holds 1 for imports and 10 for declarations:\n%s",
				got,
				output,
			)
		}
	})

	// A rule the front door has no words for prints under its own id and counts items, never under
	// another rule's heading.
	// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
	// activeSummary
	test.Run("UnusedUnknownRule", func(test *testing.T) {
		lines := []string{}
		for _, line := range contractFixture(test, "Unused.jsonl") {
			lines = append(
				lines,
				strings.ReplaceAll(line, "cohere-swift/correctness-no-unused-import", "cohere-swift/unused-parameter"),
			)
		}
		output, exitCode, renderRecordsError := renderRecords(test, engineModeCheck, lines, 0)
		if renderRecordsError != nil || exitCode != 0 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		// The dashes are escaped, not changed, as in Unused above.
		requireLines(test, output,
			"\n  cohere-swift/unused-parameter \u2014 3\n",
			"  looked at 2 files and 6 items (cohere-swift/unused-parameter)\n",
			"  not checked for unused items: 1 files (the build has not compiled it as it stands)\n",
			// The known rule beside it keeps its own words.
			"\n  declarations nothing uses \u2014 2\n",
		)
		forbidLines(test, output, "imports nothing uses")
	})

	// The contract's crash: the stream stops after the fix phase. Exit 0 or 1 without a summary is a
	// crash whatever the code says, so both are asserted.
	for _, engineExit := range []int{0, 1} {
		// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
		// activeSummary
		test.Run("CrashWithoutSummary", func(test *testing.T) {
			output, exitCode, renderRecordsError := renderRecords(
				test,
				engineModeCheck,
				contractFixture(test, "CrashWithoutSummary.jsonl"),
				engineExit,
			)
			if renderRecordsError == nil || exitCode != 1 {
				test.Fatalf("a stream with no summary exited %d with err %v\n%s", exitCode, renderRecordsError, output)
			}
			if !strings.Contains(renderRecordsError.Error(), "without a summary, so nothing was checked") {
				test.Errorf("the crash did not say nothing was checked: %v", renderRecordsError)
			}
			requireLines(test, output,
				"types did not run (the Swift engine exited",
				"  this run did not check everything",
			)
		})
	}
}

// TestSwiftRunRefusesBrokenStreams holds every refusal the contract asks for, each as a mutation of a
// real fixture. Every one must exit 1 with an error, and none may print a clean bill of health.
// Not parallel: its subtests render check runs through engineRun, whose record acceptors set the package-level
// activeSummary
func TestSwiftRunRefusesBrokenStreams(test *testing.T) {
	findings := contractFixture(test, "Findings.jsonl")
	clean := contractFixture(test, "Clean.jsonl")
	bail := contractFixture(test, "TypesBail.jsonl")
	unreadable := contractFixture(test, "Unreadable.jsonl")
	unused := contractFixture(test, "Unused.jsonl")
	last := len(findings) - 1

	replace := func(lines []string, index int, line string) []string {
		mutated := append([]string(nil), lines...)
		mutated[index] = line
		return mutated
	}
	without := func(lines []string, index int) []string {
		mutated := append([]string(nil), lines[:index]...)
		return append(mutated, lines[index+1:]...)
	}

	cases := []struct {
		name       string
		lines      []string
		engineExit int
		reason     string
	}{
		{
			"a line that is not a record",
			replace(findings, 4, "warning: the build printed to stdout"),
			1,
			"not a record",
		},
		{"a record of unknown kind", replace(findings, 4, `{"kind":"telemetry"}`), 1, "unknown kind"},
		{"provenance not first", without(findings, 0), 1, "provenance is always first"},
		{
			"a contract the front door does not speak",
			replace(findings, 0, strings.Replace(findings[0], `"contract":3`, `"contract":2`, 1)),
			1,
			"contract 2",
		},
		{
			"a provenance with no contract",
			replace(findings, 0, strings.Replace(findings[0], `"contract":3,`, "", 1)),
			1,
			"contract none",
		},
		{
			"a finding the summary does not count",
			replace(findings, last, strings.Replace(findings[last], `"findings":2`, `"findings":1`, 1)),
			1,
			"counts 1 findings and 2",
		},
		{
			"a types record that disagrees with its findings",
			replace(findings, 5, strings.Replace(findings[5], `"diagnostics":1`, `"diagnostics":0`, 1)),
			1,
			"types record counts 0",
		},
		{
			"a lint record that disagrees with its findings",
			replace(findings, 8, strings.Replace(findings[8], `"findings":1`, `"findings":3`, 1)),
			1,
			"lint record counts 3",
		},
		{"a missing phase record", without(findings, 9), 1, "expects lint"},
		{
			"phases out of order",
			replace(clean, 3, `{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":1,"detail":""}`),
			0,
			"expects fix",
		},
		{
			"an outcome the pipeline does not have",
			replace(clean, 3, strings.Replace(clean[3], `"ran"`, `"partial"`, 1)),
			0,
			"not one of",
		},
		{"a record after the summary", append(append([]string(nil), clean...), clean[1]), 0, "after the summary"},
		// The dangerous direction: lint was cut off and the summary claims the run was complete.
		{
			"a summary claiming complete over a bailed lint",
			replace(bail, len(bail)-1, strings.Replace(bail[len(bail)-1], `"complete":false`, `"complete":true`, 1)),
			1,
			"calls the run complete",
		},
		{
			"a summary whose exit code disagrees with its own findings",
			replace(findings, last, strings.Replace(findings[last], `"exitCode":1`, `"exitCode":0`, 1)),
			0,
			"summary says exit 0",
		},
		{"an engine whose exit disagrees with its summary", findings, 0, "said it would exit 1"},
		{"a summary missing its fields", replace(findings, last, `{"kind":"summary"}`), 1, "missing findings"},
		// changedFiles, additive within contract 3: "N cohered" is the list, so the list must be the count.
		{
			"a changed-file list shorter than its count",
			replace(findings, 2, strings.Replace(findings[2], `"filesRewritten":1`, `"filesRewritten":2`, 1)),
			1,
			"counts 2 files rewritten and lists 1",
		},
		{"a file listed twice", replace(findings, 2, strings.Replace(
			strings.Replace(findings[2], `"filesRewritten":1`, `"filesRewritten":2`, 1),
			`"formatted":true}]`,
			`"formatted":true},{"file":"/project/Sources/Example/ByteRing.swift","fixedBy":{},"formatted":true}]`,
			1,
		)), 1, "lists /project/Sources/Example/ByteRing.swift twice"},
		{"a file nothing changed", replace(findings, 2, strings.Replace(
			strings.Replace(findings[2], `{"cohere-swift/legacy-constructors":1}`, `{}`, 1),
			`"formatted":true`,
			`"formatted":false`,
			1,
		)), 1, "rewritten by no fix and not by the formatter"},
		// Contract 2's unreadable record: a summary may not call the run complete over one, and it has one place.
		{
			"a summary claiming complete over an unreadable file",
			replace(unreadable, len(unreadable)-1, `{"kind":"summary","findings":0,"complete":true,"exitCode":0}`),
			0,
			"calls the run complete",
		},
		{
			"an unreadable record after a phase",
			append(append(append([]string(nil), unreadable[:5]...), unreadable[2]), unreadable[5:]...),
			1,
			"after phase fix",
		},
		{
			"an unreadable record before the project",
			append([]string{unreadable[0], unreadable[2]}, unreadable[1:]...),
			1,
			"before the project record",
		},
		// The --unused report: well-formed, in its place, and counted by its coverage record.
		{
			"an unused record that does not decode",
			replace(unused, 8, `{"kind":"unused","line":"one"}`),
			0,
			"does not decode",
		},
		{
			"an unused record with no subject",
			replace(unused, 8, strings.Replace(unused[8], `"subject":`, `"unnamed":`, 1)),
			0,
			"missing its file, position, rule or subject",
		},
		{
			"an unused record before the lint phase",
			append(append(append([]string(nil), unused[:7]...), unused[8], unused[7]), unused[9:]...),
			0,
			"outside the unused phase",
		},
		{
			"an unused coverage count that disagrees",
			replace(unused, 11, strings.Replace(unused[11], `"found":3`, `"found":4`, 1)),
			0,
			"counts 4 found and 3",
		},
		// The fixture holds two rules: unused-import's records at 8-10 and its coverage at 11, then
		// unused-declaration's records at 12-13 and its coverage at 14.
		{"unused records with no coverage record", without(unused, 14), 0, "with no unusedCoverage record"},
		{
			"one rule's records interleaved before another's coverage",
			without(unused, 11),
			0,
			"of another rule arrived before the coverage record",
		},
		{
			"an unused phase that ran with no coverage",
			without(without(without(without(without(without(without(unused, 14), 13), 12), 11), 10), 9), 8),
			0,
			"no unusedCoverage record said",
		},
	}

	for _, testCase := range cases {
		// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
		// activeSummary
		test.Run(testCase.name, func(test *testing.T) {
			output, exitCode, renderRecordsError := renderRecords(
				test,
				engineModeCheck,
				testCase.lines,
				testCase.engineExit,
			)
			if renderRecordsError == nil || exitCode != 1 {
				test.Fatalf("accepted a broken stream: exit %d, err %v\n%s", exitCode, renderRecordsError, output)
			}
			if !strings.Contains(renderRecordsError.Error(), testCase.reason) {
				test.Errorf("refused for the wrong reason: %v, want one naming %q", renderRecordsError, testCase.reason)
			}
		})
	}
}

// TestSwiftRunGroupsExcludedFiles holds both sides of the limit: a reason with many files is one
// counted line, and a reason with few still names each file.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary
func TestSwiftRunGroupsExcludedFiles(test *testing.T) {
	clean := contractFixture(test, "Clean.jsonl")
	vendored := []string{}
	for index := range excludedNamedLimit + 1 {
		vendored = append(
			vendored,
			`{"file":"/project/Vendor/F`+string(rune('a'+index))+`.swift","reason":"vendored under Vendor/"}`,
		)
	}
	clean[1] = strings.Replace(clean[1], `"excluded":[`, `"excluded":[`+strings.Join(vendored, ",")+",", 1)

	output, exitCode, renderRecordsError := renderRecords(test, engineModeCheck, clean, 0)
	if renderRecordsError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
	}
	requireLines(test, output,
		"  not checked: 6 files (vendored under Vendor/)\n",
		"  not checked: /project/Sources/Example/Generated.swift (ignored by git)\n",
	)
	forbidLines(test, output, "/project/Vendor/Fa.swift")
}

// excludedFile is one file a project record lists as not checked, and why.
type excludedFile struct {
	file   string
	reason string
}

// declarationsOnlyReason is the engine's reason for a file no target compiles that holds only declarations, so
// it is no standalone script to check alone (swift/Sources/CohereSwift/Project/FileSet.swift, #13nbb1s). A script
// is checked, and the bare `no target of this package compiles it` is left for a file below a project of its own.
const declarationsOnlyReason = "no target of this package compiles it, and it holds only declarations, so it is no script to check alone"

// projectRecordAt is a contract fixture's project record moved to root, its excluded files replaced, so a
// test can state which files the engine left out and why.
func projectRecordAt(test *testing.T, record string, root string, excluded []excludedFile) string {
	test.Helper()
	// Each field stays as the fixture wrote it, so the record passes through whole but for the two replaced.
	var fields map[string]json.RawMessage
	if decodeError := json.Unmarshal([]byte(record), &fields); decodeError != nil {
		test.Fatal(decodeError)
	}
	var kind string
	if kindError := json.Unmarshal(fields["kind"], &kind); kindError != nil || kind != "project" {
		test.Fatalf("not a project record: %s", record)
	}
	entries := make([]map[string]string, 0, len(excluded))
	for _, entry := range excluded {
		entries = append(entries, map[string]string{"file": entry.file, "reason": entry.reason})
	}
	encodedRoot, rootError := json.Marshal(root)
	if rootError != nil {
		test.Fatal(rootError)
	}
	encodedEntries, entriesError := json.Marshal(entries)
	if entriesError != nil {
		test.Fatal(entriesError)
	}
	fields["root"], fields["excluded"] = encodedRoot, encodedEntries
	encoded, encodeError := json.Marshal(fields)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	return string(encoded)
}

// A Swift run that yields a directory to the Swift project below it, an Xcode project or a nested package
// running on its own, leaves every excluded file under it out of its notes, whatever the reason, since that
// project's run accounts for them; the engine spells the path as Foundation does, `/tmp` where this process
// holds `/private/tmp`, so both sides are resolved. What remains is the gap, counted for the footer, unless its
// reason is one the engine gives for files nobody expects checked; a reason it has never given before counts,
// so a reworded sentence makes the gap louder. A file the engine could not read is this run's own and is never
// left out. None of it moves the exit code (#ybspptb).
// Not parallel: it renders check runs through engineRun, whose record acceptors set the package-level
// activeSummary, which it reads back
func TestSwiftRunLeavesYieldedFilesOutAndCountsTheRestAsAGap(test *testing.T) {
	resolvedRoot, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	alias := filepath.Join(test.TempDir(), "alias")
	if symlinkError := os.Symlink(resolvedRoot, alias); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	// The engine's spellings of the same directory: through a symbolic link on any platform, and on macOS,
	// where the temporary directory is under /private, the spelling Foundation gives it without the prefix.
	spellings := map[string]string{"through a symbolic link": alias}
	if trimmed, isPrivate := strings.CutPrefix(resolvedRoot, "/private/"); isPrivate {
		spellings["as /tmp is spelled for /private/tmp"] = "/" + trimmed
	}
	writeTree(test, resolvedRoot, map[string]string{
		"CapturePhone/App/App.swift": "let app = 1\n",
		"Tools/Sim/Package.swift":    discoveryPackage,
		"Tools/Sim/Broken.swift":     "let broken = 1\n",
	})
	yielded := []string{filepath.Join(resolvedRoot, "CapturePhone"), filepath.Join(resolvedRoot, "Tools", "Sim")}

	for name, engineRoot := range spellings {
		excluded := []excludedFile{
			{engineRoot + "/CapturePhone/App/App.swift", "no target of this package compiles it"},
			{engineRoot + "/Tools/Sim/Package.swift", "a package manifest"},
			{engineRoot + "/Stray/Leftover.swift", declarationsOnlyReason},
			{engineRoot + "/Stray/odd.swift", "a reason no engine has given yet"},
			{engineRoot + "/Package.swift", "a package manifest"},
			{engineRoot + "/Vendor/Kit/Kit.swift", "vendored under Vendor/"},
			{engineRoot + "/Scratch.swift", "ignored by git"},
		}

		// Not parallel: it renders a check run through engineRun and reads back the package-level activeSummary
		test.Run(name, func(test *testing.T) {
			clean := contractFixture(test, "Clean.jsonl")
			clean[1] = projectRecordAt(test, clean[1], engineRoot, excluded)

			activeSummary = runSummary{}
			output, exitCode, renderRecordsError := renderRecordsYielding(test, engineModeCheck, clean, 0, yielded)
			if renderRecordsError != nil || exitCode != 0 {
				test.Fatalf(
					"a clean run with a gap exited %d, err %v, want the 0 it exits without one:\n%s",
					exitCode,
					renderRecordsError,
					output,
				)
			}
			forbidLines(test, output, "CapturePhone/App/App.swift", "Tools/Sim/Package.swift")
			requireLines(test, output,
				"  not checked: "+engineRoot+"/Stray/Leftover.swift ("+declarationsOnlyReason+")\n",
				"  not checked: "+engineRoot+"/Stray/odd.swift (a reason no engine has given yet)\n",
				"  not checked: "+engineRoot+"/Package.swift (a package manifest)\n",
				"  not checked: "+engineRoot+"/Vendor/Kit/Kit.swift (vendored under Vendor/)\n",
				"  not checked: "+engineRoot+"/Scratch.swift (ignored by git)\n",
			)
			wantGap := []uncheckedEngineFile{
				{File: engineRoot + "/Stray/Leftover.swift", Reason: declarationsOnlyReason, Language: "Swift"},
				{File: engineRoot + "/Stray/odd.swift", Reason: "a reason no engine has given yet", Language: "Swift"},
			}
			if !reflect.DeepEqual(activeSummary.Gaps.Unchecked, wantGap) {
				test.Errorf(
					"the gap is %v, want %v: Leftover.swift and odd.swift, and none of the three expected reasons",
					activeSummary.Gaps.Unchecked,
					wantGap,
				)
			}
			if markers := uncheckedMarkers(activeSummary); !slices.Contains(markers, "⚠ 2 Swift files not checked") {
				test.Errorf("the footer's markers are %q, want the gap among them", markers)
			}

			// The control: the same stream yielding nothing names every file, so what was left out above was
			// the yield at work.
			activeSummary = runSummary{}
			output, _, renderRecordsError = renderRecordsYielding(test, engineModeCheck, clean, 0, nil)
			if renderRecordsError != nil {
				test.Fatal(renderRecordsError)
			}
			requireLines(test, output,
				"  not checked: "+engineRoot+"/CapturePhone/App/App.swift (no target of this package compiles it)\n",
				"  not checked: "+engineRoot+"/Tools/Sim/Package.swift (a package manifest)\n",
			)
			if len(activeSummary.Gaps.Unchecked) != 3 {
				test.Errorf(
					"yielding nothing listed %v as not checked, want 3 files",
					activeSummary.Gaps.Unchecked,
				)
			}

			// A file the engine could not read under a yielded directory is still named, and the run is
			// incomplete with the exit 1 it gives without the yield.
			unreadable := contractFixture(test, "Unreadable.jsonl")
			unreadable[1] = projectRecordAt(test, unreadable[1], engineRoot, excluded)
			unreadable[2] = strings.Replace(
				unreadable[2],
				"/project/Sources/Example/Latin1.swift",
				engineRoot+"/Tools/Sim/Broken.swift",
				1,
			)
			activeSummary = runSummary{}
			output, exitCode, renderRecordsError = renderRecordsYielding(test, engineModeCheck, unreadable, 1, yielded)
			if renderRecordsError != nil || exitCode != 1 {
				test.Fatalf("an unreadable file exited %d, err %v, want 1:\n%s", exitCode, renderRecordsError, output)
			}
			requireLines(test, output, "  not checked: "+engineRoot+"/Tools/Sim/Broken.swift (could not be read: ")
			forbidLines(test, output, "CapturePhone/App/App.swift")
		})
	}
}

// The contract's clean fixture leaves its one file out for a reason the engine gives today, `ignored by git`,
// which nobody expects a run to check, so a stand-in run on it is clean with no gap. It carried `marked //
// @generated`, which the engine stopped giving at d3bd032b, and every stand-in run on it showed one Swift file
// not checked (#zskc1pa). A reason the engine has never given is still a gap: see
// TestSwiftRunLeavesYieldedFilesOutAndCountsTheRestAsAGap.
// Not parallel: it renders a check run through engineRun and reads back the package-level activeSummary
func TestSwiftRunFindsNoGapInTheCleanFixture(test *testing.T) {
	activeSummary = runSummary{}
	output, exitCode, renderRecordsError := renderRecords(
		test,
		engineModeCheck,
		contractFixture(test, "Clean.jsonl"),
		0,
	)
	if renderRecordsError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
	}
	if len(activeSummary.Gaps.Unchecked) != 0 {
		test.Errorf("the clean fixture's gap is %v, want none", activeSummary.Gaps.Unchecked)
	}
	for _, marker := range uncheckedMarkers(activeSummary) {
		if strings.Contains(marker, "Swift file") {
			test.Errorf("the footer marks %q for the clean fixture, which leaves out only what git ignores", marker)
		}
	}
}

// A gap is named file by file, however many files share its reason, in the account `--verbose` prints and in
// the list `--json` writes, while a reason nobody expects checked still collapses to a count past the limit. A
// file of the gap's reason under a yielded directory is in neither, since the run there accounts for it.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
// activeSummary, which it reads back
func TestSwiftRunNamesEveryFileOfTheGap(test *testing.T) {
	excluded := []excludedFile{{"/project/App/Main.swift", "no target of this package compiles it"}}
	wantGap := []uncheckedEngineFile{}
	for index := range excludedNamedLimit + 1 {
		letter := string(rune('a' + index))
		excluded = append(excluded,
			excludedFile{"/project/Stray/S" + letter + ".swift", declarationsOnlyReason},
			excludedFile{"/project/Vendor/V" + letter + ".swift", "vendored under Vendor/"},
		)
		wantGap = append(
			wantGap,
			uncheckedEngineFile{
				File:     "/project/Stray/S" + letter + ".swift",
				Reason:   declarationsOnlyReason,
				Language: "Swift",
			},
		)
	}
	clean := contractFixture(test, "Clean.jsonl")
	clean[1] = projectRecordAt(test, clean[1], "/project", excluded)

	activeSummary = runSummary{}
	output, exitCode, renderRecordsError := renderRecordsYielding(
		test,
		engineModeCheck,
		clean,
		0,
		[]string{"/project/App"},
	)
	if renderRecordsError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
	}
	for _, file := range wantGap {
		requireLines(test, output, "  not checked: "+file.File+" ("+file.Reason+")\n")
	}
	requireLines(test, output, "  not checked: 6 files (vendored under Vendor/)\n")
	forbidLines(
		test,
		output,
		"files ("+declarationsOnlyReason+")",
		"/project/Vendor/Va.swift",
		"/project/App/Main.swift",
	)
	if !reflect.DeepEqual(activeSummary.Gaps.Unchecked, wantGap) {
		test.Errorf("the gap is %v, want %v", activeSummary.Gaps.Unchecked, wantGap)
	}
	if markers := uncheckedMarkers(activeSummary); !slices.Contains(markers, "⚠ 6 Swift files not checked") {
		test.Errorf("the footer's markers are %q, want the six files of the gap counted", markers)
	}

	// The list as `--json` writes it: each file with its reason and its language, under the key the schema names.
	encoded, encodeError := json.Marshal(summaryAsJson(activeSummary))
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	var decoded struct {
		Gaps struct {
			Unchecked []map[string]string `json:"unchecked"`
		} `json:"gaps"`
	}
	if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
		test.Fatal(decodeError)
	}
	wantJson := []map[string]string{}
	for _, file := range wantGap {
		wantJson = append(wantJson, map[string]string{"file": file.File, "reason": file.Reason, "language": "Swift"})
	}
	if !reflect.DeepEqual(decoded.Gaps.Unchecked, wantJson) {
		test.Errorf("under --json the gap is %v, want %v:\n%s", decoded.Gaps.Unchecked, wantJson, encoded)
	}
}

// TestSwiftRunBelievesAnEngineThatFellShort holds the allowed direction of the completeness check: an
// engine that says it did not check everything is believed even when its records show no gap, because
// it can see gaps no record carries, such as a file it could not read.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary
func TestSwiftRunBelievesAnEngineThatFellShort(test *testing.T) {
	clean := contractFixture(test, "Clean.jsonl")
	last := len(clean) - 1
	clean[last] = strings.Replace(
		strings.Replace(clean[last], `"complete":true`, `"complete":false`, 1),
		`"exitCode":0`,
		`"exitCode":1`,
		1,
	)

	output, exitCode, renderRecordsError := renderRecords(test, engineModeCheck, clean, 1)
	if renderRecordsError != nil || exitCode != 1 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
	}
	requireLines(
		test,
		output,
		"  this run did not check everything: the Swift engine reported that it fell short without a record saying where\n",
	)
}

// TestSwiftRunListingModes holds the two runs that end without a summary on purpose.
func TestSwiftRunListingModes(test *testing.T) {
	test.Parallel()
	provenance := contractFixture(test, "Clean.jsonl")[0]

	test.Run("version", func(test *testing.T) {
		test.Parallel()
		output, exitCode, renderRecordsError := renderRecords(test, engineModeVersion, []string{provenance}, 0)
		if renderRecordsError != nil || exitCode != 0 {
			test.Fatalf("exit %d, err %v", exitCode, renderRecordsError)
		}
		requireLines(
			test,
			output,
			"swift engine: cohere-swift 0.1.0 (commit dev, swiftlang-6.4.0.34.1, swift-syntax 604.0.0, swift-format 604.0.0)\n",
		)
	})

	test.Run("rules, sorted", func(test *testing.T) {
		test.Parallel()
		lines := []string{
			provenance,
			`{"kind":"rule","name":"cohere-swift/no-try-bang","severity":"error"}`,
			`{"kind":"rule","name":"cohere-swift/force-unwrapping","severity":"warning"}`,
		}
		output, exitCode, renderRecordsError := renderRecords(test, engineModeRules, lines, 0)
		if renderRecordsError != nil || exitCode != 0 {
			test.Fatalf("exit %d, err %v", exitCode, renderRecordsError)
		}
		if output != "cohere-swift/force-unwrapping\ncohere-swift/no-try-bang\n" {
			test.Errorf("rules printed as %q", output)
		}
		enabled, _, renderRecordsError := renderRecords(test, engineModeRulesEnabled, lines, 0)
		if renderRecordsError != nil ||
			enabled != "cohere-swift/force-unwrapping\twarning\ncohere-swift/no-try-bang\terror\n" {
			test.Errorf("rules-enabled printed as %q (err %v)", enabled, renderRecordsError)
		}
	})

	test.Run("a finding in a listing is refused", func(test *testing.T) {
		test.Parallel()
		findings := contractFixture(test, "Findings.jsonl")
		_, exitCode, renderRecordsError := renderRecords(test, engineModeRules, []string{provenance, findings[4]}, 0)
		if renderRecordsError == nil || exitCode != 1 {
			test.Fatalf("a finding in a rules listing was accepted")
		}
	})

	test.Run("an engine that could not run", func(test *testing.T) {
		test.Parallel()
		_, exitCode, renderRecordsError := renderRecords(test, engineModeCheck, []string{provenance}, 2)
		if renderRecordsError == nil ||
			exitCode != 1 ||
			!strings.Contains(renderRecordsError.Error(), "did not run, so nothing was checked") {
			test.Fatalf("exit 2 was rendered as exit %d, err %v", exitCode, renderRecordsError)
		}
	})
}

func TestSwiftEngineArguments(test *testing.T) {
	test.Parallel()
	location := projectLocation{
		Root:               "/work/macos",
		Engine:             engineSwift,
		LintConfigFileName: "/work/macos/sub/Local.json",
		ArgumentBase:       "/work/macos/sub",
	}
	values := map[string]string{"no-fix": "true", "single-threaded": "false", "fix-passes": "4", "rules": "true"}
	value := func(name string) string { return values[name] }

	arguments, mode, engineArgumentsError := engineArguments(swiftLanguage, swiftFlags, location,
		map[string]bool{"no-fix": true, "single-threaded": true, "fix-passes": true, "lint-config": true},
		value, []string{"Thing.swift", "/abs/Other.swift"})
	if engineArgumentsError != nil {
		test.Fatal(engineArgumentsError)
	}
	want := "--contract 3 --root /work/macos --no-fix --lint-config /work/macos/sub/Local.json --fix-passes 4 /work/macos/sub/Thing.swift /abs/Other.swift"
	if strings.Join(arguments, " ") != want || mode != engineModeCheck {
		// `--single-threaded=false` was typed and is false, so it is not forwarded as a switch.
		test.Errorf("arguments %q mode %s, want %q", strings.Join(arguments, " "), mode, want)
	}

	_, mode, rulesError := engineArguments(swiftLanguage, swiftFlags, location, map[string]bool{"rules": true}, value,
		nil)
	if rulesError != nil {
		test.Fatal(rulesError)
	}
	if mode != engineModeRules {
		test.Errorf("--rules ran in mode %s", mode)
	}

	if _, _, engineArgumentsError := engineArguments(
		swiftLanguage,
		swiftFlags,
		location,
		map[string]bool{"explain": true},
		value,
		nil,
	); engineArgumentsError == nil || !strings.Contains(engineArgumentsError.Error(), "--explain") {
		test.Errorf("--explain was not refused by name: %v", engineArgumentsError)
	}
}

// A flag the engine does not read and the front door does not answer is refused by name, never dropped: a
// dropped --format-only made a format gate type-check and lint the whole package (#nqb3mjv). A switch typed
// as false asks for nothing and passes, and the flags the front door answers itself pass too. --format-only
// and --no-format reach the engine, which reads them with TypeScript's meaning since c024df4b, and
// --no-cache since 9def2dc1.
func TestSwiftEngineArgumentsRefuseWhatNothingHonors(test *testing.T) {
	test.Parallel()
	location := projectLocation{Root: "/work/macos", Engine: engineSwift, ArgumentBase: "/work/macos"}
	values := map[string]string{"no-format": "false", "verbose": "true", "phases": "true", "coverage": "true"}
	value := func(name string) string {
		if set, found := values[name]; found {
			return set
		}
		return "true"
	}

	for flag, says := range map[string]string{
		"print-config":   "--print-config is not implemented for Swift yet",
		"stdin-filepath": "--stdin-filepath is not implemented for Swift yet",
		"cache-dump":     "--cache-dump is not implemented for Swift yet",
	} {
		if _, _, engineArgumentsError := engineArguments(
			swiftLanguage,
			swiftFlags,
			location,
			map[string]bool{"no-fix": true, flag: true},
			value,
			nil,
		); engineArgumentsError == nil ||
			!strings.Contains(engineArgumentsError.Error(), says) ||
			!strings.Contains(engineArgumentsError.Error(), "nothing was checked") {
			test.Errorf("--%s was not refused by name: %v", flag, engineArgumentsError)
		}
	}

	for _, flag := range []string{"format-only", "no-format", "no-cache"} {
		values[flag] = "true"
		arguments, _, engineArgumentsError := engineArguments(
			swiftLanguage,
			swiftFlags,
			location,
			map[string]bool{"no-fix": true, flag: true},
			value,
			nil,
		)
		if engineArgumentsError != nil ||
			strings.Join(arguments, " ") != "--contract 3 --root /work/macos --no-fix --"+flag {
			test.Errorf(
				"--%s did not reach the engine: %q (%v)",
				flag,
				strings.Join(arguments, " "),
				engineArgumentsError,
			)
		}
	}
	values["no-format"] = "false"

	given := map[string]bool{
		"no-fix":    true,
		"no-format": true,
		"verbose":   true,
		"phases":    true,
		"coverage":  true,
		"directory": true,
	}
	arguments, _, engineArgumentsError := engineArguments(swiftLanguage, swiftFlags, location, given, value, nil)
	if engineArgumentsError != nil || strings.Join(arguments, " ") != "--contract 3 --root /work/macos --no-fix" {
		test.Errorf(
			"the flags the front door answers were not passed over: %q (%v)",
			strings.Join(arguments, " "),
			engineArgumentsError,
		)
	}
}

// TestSwiftRunListsTheFilesItRewrote: a fix record with its list fills the run summary the footer counts as
// cohered, each path relative to the package, with the fixes by rule. A record from an engine before the list
// has none, and the summary is left unlisted, never set to no files, so the footer prints no cohered count.
// Not parallel: it assigns the package-level activeSummary and reads back what the Swift record acceptors set
// there
func TestSwiftRunListsTheFilesItRewrote(test *testing.T) {
	activeSummary = runSummary{}
	if _, _, renderRecordsError := renderRecords(
		test,
		engineModeCheck,
		contractFixture(test, "Findings.jsonl"),
		1,
	); renderRecordsError != nil {
		test.Fatal(renderRecordsError)
	}
	want := []changedFile{{
		Path:      "Sources/Example/ByteRing.swift",
		Fixed:     true,
		Formatted: true,
		FixedBy:   map[string]int{"cohere-swift/legacy-constructors": 1},
	}}
	if !reflect.DeepEqual(activeSummary.Changed, want) {
		test.Errorf("the summary lists %+v, expected %+v", activeSummary.Changed, want)
	}
	if activeSummary.cohered() != 1 {
		test.Errorf("cohered %d, expected the one file listed", activeSummary.cohered())
	}

	activeSummary = runSummary{}
	output, _, renderRecordsError := renderRecords(test, engineModeCheck, contractFixture(test, "Clean.jsonl"), 0)
	if renderRecordsError != nil {
		test.Fatal(renderRecordsError)
	}
	if activeSummary.Changed != nil {
		test.Errorf("a stream with no list set the summary to %+v; it is not listed, not empty", activeSummary.Changed)
	}
	forbidLines(test, output, "cohered")
}

// A Swift run whose fix phase left a file unwritten, since it is reached through a link to outside the root (#723rzxm),
// names it with its real path in the account `--verbose` prints, counts it in the footer every view ends with, lists it
// in `--json`'s gaps, and exits 1, incomplete. The same stream calling itself complete is refused: its own fix record
// shows a gap.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestSwiftRunNamesTheFilesItDidNotWrite(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	lines := contractFixture(test, "NotWritten.jsonl")
	render := func(mode outputMode) string {
		test.Helper()
		activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
		output, exitCode, renderRecordsError := renderRecords(test, engineModeCheck, lines, 1)
		if renderRecordsError != nil || exitCode != 1 {
			test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
		}
		return output
	}

	verbose := render(outputVerbose)
	requireLines(test, verbose,
		"  not written: /project/Sources/Example/Linked/Outside.swift → /elsewhere/Outside.swift\n",
		"⚠ not written: 1 file reached through a link outside the root",
	)
	want := []notWrittenFile{
		{File: "/project/Sources/Example/Linked/Outside.swift", RealPath: "/elsewhere/Outside.swift"},
	}
	if !reflect.DeepEqual(activeSummary.Gaps.NotWritten, want) {
		test.Errorf("the summary's gap is %+v, want %+v", activeSummary.Gaps.NotWritten, want)
	}
	requireLines(test, render(outputHuman), "⚠ not written: 1 file reached through a link outside the root")

	summaryLine := summaryJsonOf(test, render(outputJson))
	var decoded struct {
		Gaps struct {
			NotWritten []notWrittenFile `json:"notWritten"`
		} `json:"gaps"`
	}
	if decodeError := json.Unmarshal([]byte(summaryLine), &decoded); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, summaryLine)
	}
	if !reflect.DeepEqual(decoded.Gaps.NotWritten, want) {
		test.Errorf("under --json the gap is %+v:\n%s", decoded.Gaps.NotWritten, summaryLine)
	}

	claimed := slices.Clone(lines)
	claimed[len(claimed)-1] = `{"kind":"summary","findings":2,"complete":true,"exitCode":1}`
	activeOutput, activeSummary = outputSettings{Mode: outputHuman}, runSummary{}
	if _, _, renderRecordsError := renderRecords(test, engineModeCheck, claimed, 1); renderRecordsError == nil ||
		!strings.Contains(renderRecordsError.Error(), "calls the run complete") {
		test.Errorf("a stream calling itself complete over a file not written was not refused: %v", renderRecordsError)
	}
}

// TestEngineRunRendersAGoStreamInGoWords renders a clean Go engine's stream through the renderer Swift
// uses, and holds that every sentence names the Go engine's world: a Swift word in a Go run's output would
// tell a reader the wrong engine checked their code.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
// activeSummary
func TestEngineRunRendersAGoStreamInGoWords(test *testing.T) {
	activeSummary = runSummary{}
	output, exitCode, renderRecordsInError := renderRecordsIn(test, goLanguage("module"), engineModeCheck,
		contractFixture(test, "GoClean.jsonl"), 0, nil)
	if renderRecordsInError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsInError, output)
	}
	requireLines(test, output,
		"module described in 96ms",
		"4 Go files in the module, 3 of them ours\n",
		"types: 0 diagnostics over 3 files in 240ms\n",
		"  not checked: /project/internal/schema/schema_generated.go (ignored by git)\n",
		" module 96ms",
	)
	forbidLines(test, output, "Swift", "swift", "package")
	// The fixture leaves out only a file git ignores, which nobody expects checked, so a clean run keeps no gap.
	// It carried `marked // Code generated`, which the engine stopped giving when a generated file stayed ours,
	// and every other reason is a gap (TestAGoRunNamesAFileOnlyAnotherPlatformBuilds).
	if len(activeSummary.Gaps.Unchecked) != 0 {
		test.Errorf("a clean Go run kept the gap %v", activeSummary.Gaps.Unchecked)
	}
}

// TestAGoRunNamesAFileOnlyAnotherPlatformBuilds: the Go engine checks the host's build alone, so a file only another
// platform builds was never loaded, whatever it holds, and an `_windows.go` file's finding is one no run on a Mac
// reports. A green run there names each such file as a Go file not checked: an `_windows.go` file, a file
// `//go:build linux`, and one that imports "C" while cgo is off, in the footer, under `--json` with its reason, and
// in the account file by file. It stays green with the exit it had, since a gap is never a failure. A file git
// ignores is one nobody expects checked and stays out of the gap, and so is one a run under `--other-builds` says
// another build checked, whose reason names the build and whose account line says that build checked it
// (#72n9mq0).
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level
// activeSummary, which it reads back
func TestAGoRunNamesAFileOnlyAnotherPlatformBuilds(test *testing.T) {
	// The engine's words on a Mac (internal/go_engine/load/excluded.go).
	const constraintsReason = "excluded by build constraints on darwin/arm64"
	const cgoReason = `imports "C", and cgo is off for darwin/arm64`
	stream := contractFixture(test, "GoClean.jsonl")
	stream[1] = projectRecordAt(test, stream[1], "/project", []excludedFile{
		{"/project/internal/native/native.go", cgoReason},
		{"/project/internal/open/open_linux.go", constraintsReason},
		{"/project/internal/open/open_windows.go", constraintsReason},
		{"/project/internal/separator/separator_windows.go", "checked by the windows/arm64 build"},
		{"/project/scratch.go", "ignored by git"},
	})

	activeSummary = runSummary{}
	output, exitCode, renderRecordsInError := renderRecordsIn(test, goLanguage("module"), engineModeCheck, stream, 0,
		nil)
	if renderRecordsInError != nil || exitCode != 0 {
		test.Fatalf("a clean Go run with a gap exited %d, err %v, want the 0 it exits without one:\n%s",
			exitCode, renderRecordsInError, output)
	}
	wantGap := []uncheckedEngineFile{
		{File: "/project/internal/native/native.go", Reason: cgoReason, Language: "Go"},
		{File: "/project/internal/open/open_linux.go", Reason: constraintsReason, Language: "Go"},
		{File: "/project/internal/open/open_windows.go", Reason: constraintsReason, Language: "Go"},
	}
	if !reflect.DeepEqual(activeSummary.Gaps.Unchecked, wantGap) {
		test.Errorf("the gap is %v, want %v: every file only another platform builds, and not what git ignores "+
			"or another build checked",
			activeSummary.Gaps.Unchecked, wantGap)
	}
	for _, file := range wantGap {
		requireLines(test, output, "  not checked: "+file.File+" ("+file.Reason+")\n")
	}
	// The file another build checked was checked, and the account says by which build, never "not checked".
	requireLines(test, output,
		"  checked by the windows/arm64 build: /project/internal/separator/separator_windows.go\n")
	forbidLines(test, output, "not checked: /project/internal/separator/separator_windows.go")

	if activeSummary.failed() {
		test.Errorf("the gap failed the run: %+v", activeSummary.Gaps)
	}
	line := footer(activeSummary, plain, footerOptions{})
	if !strings.HasPrefix(line, "✓ 💎") || !strings.Contains(line, " • ⚠ 3 Go files not checked") {
		test.Errorf("the footer is %q, want a green verdict with the three files counted in Go's words", line)
	}
	if strings.Contains(line, "Swift") {
		test.Errorf("the footer counts a Go run's files in Swift's words: %q", line)
	}

	// The list as `--json` writes it: each file with its reason and its language, under the key the schema names.
	encoded, encodeError := json.Marshal(summaryAsJson(activeSummary))
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	var decoded struct {
		Verdict string `json:"verdict"`
		Gaps    struct {
			Unchecked []map[string]string `json:"unchecked"`
		} `json:"gaps"`
	}
	if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
		test.Fatal(decodeError)
	}
	wantJson := []map[string]string{}
	for _, file := range wantGap {
		wantJson = append(wantJson, map[string]string{"file": file.File, "reason": file.Reason, "language": "Go"})
	}
	if !reflect.DeepEqual(decoded.Gaps.Unchecked, wantJson) {
		test.Errorf("under --json the gap is %v, want %v:\n%s", decoded.Gaps.Unchecked, wantJson, encoded)
	}
	if decoded.Verdict != "pass" {
		test.Errorf("under --json the verdict is %q, want the pass a gap never turns:\n%s", decoded.Verdict, encoded)
	}
}

// goStreamWithheld is the Go contract's clean stream with the two withheld fields added to its types record as
// given, as a Linux load reaching `net` writes them.
func goStreamWithheld(test *testing.T, withheldFields string) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	for index, line := range lines {
		if strings.HasPrefix(line, `{"kind":"types"`) {
			lines[index] = strings.TrimSuffix(line, "}") + "," + withheldFields + "}"
			return lines
		}
	}
	test.Fatal("GoClean.jsonl holds no types record")
	return nil
}

// renderGoStreamAs renders a Go stream under one output mode from a fresh summary, and gives the output with the
// summary the run left.
func renderGoStreamAs(test *testing.T, mode outputMode, lines []string) (string, runSummary) {
	test.Helper()
	return renderGoStreamStartedAt(test, mode, lines, time.Now())
}

// renderGoStreamStartedAt is renderGoStreamAs for a process that started at processStart (see renderRecordsStartedAt).
func renderGoStreamStartedAt(
	test *testing.T,
	mode outputMode,
	lines []string,
	processStart time.Time,
) (string, runSummary) {
	test.Helper()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	output, exitCode, renderRecordsError := renderRecordsStartedAt(
		test,
		goLanguage("module"),
		engineModeCheck,
		lines,
		0,
		nil,
		processStart,
	)
	if renderRecordsError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsError, output)
	}
	return output, activeSummary
}

// summaryJsonOf is the summary line a `--json` run printed, or a failure naming the output when there is none.
func summaryJsonOf(test *testing.T, output string) string {
	test.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"kind":"summary"`) {
			return line
		}
	}
	test.Fatalf("no summary line in:\n%s", output)
	return ""
}

// A Go run that withheld type errors in a dependency's cgo files, and gave some of its declarations without bodies,
// says so: one count line under the types line and each file by name in the account `--verbose` prints, one
// marker in the footer every view ends with, both fields in `--json`'s gaps, and neither fails the run, since
// what was withheld is not the project's to fix.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestEngineRunSaysWhatAGoRunWithheldForCgo(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	withheld := goStreamWithheld(test,
		`"withheldInDependencyCgo":[{"package":"net","file":"/goroot/src/net/cgo_unix.go","errors":1}],`+
			`"withheldDependencyCgoDeclarations":14`)

	verbose, summary := renderGoStreamAs(test, outputVerbose, withheld)
	requireLines(test, verbose,
		"types: 0 diagnostics over 3 files in 240ms\n"+
			"  types: withheld 1 type error in 1 file of dependencies with cgo, and 14 of their declarations were "+
			"given without bodies or initial values, since no cgo tool ran\n"+
			"  types: withheld 1 type error in /goroot/src/net/cgo_unix.go (package net)\n",
	)
	wantFiles := []withheldCgoFile{{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 1}}
	if !reflect.DeepEqual(summary.Gaps.WithheldInDependencyCgo, wantFiles) {
		test.Errorf("the gap's files are %v, want %v", summary.Gaps.WithheldInDependencyCgo, wantFiles)
	}
	if summary.Gaps.WithheldDependencyCgoDeclarations != 14 {
		test.Errorf("the gap counts %d declarations, want 14", summary.Gaps.WithheldDependencyCgoDeclarations)
	}
	if summary.failed() {
		test.Errorf("what a run withheld for cgo failed it: %+v", summary.Gaps)
	}

	const marker = " • ⚠ cgo withheld: 1 type error in 1 dependency file, 14 declarations"
	requireLines(test, verbose, marker)
	human, _ := renderGoStreamAs(test, outputHuman, withheld)
	requireLines(test, human, marker)
	forbidLines(test, human, "types: withheld", "cgo_unix.go")

	jsonOutput, _ := renderGoStreamAs(test, outputJson, withheld)
	var decoded struct {
		Verdict string `json:"verdict"`
		Gaps    struct {
			WithheldInDependencyCgo           []withheldCgoFile `json:"withheldInDependencyCgo"`
			WithheldDependencyCgoDeclarations int               `json:"withheldDependencyCgoDeclarations"`
		} `json:"gaps"`
	}
	summaryLine := summaryJsonOf(test, jsonOutput)
	if decodeError := json.Unmarshal([]byte(summaryLine), &decoded); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, summaryLine)
	}
	if !reflect.DeepEqual(decoded.Gaps.WithheldInDependencyCgo, wantFiles) ||
		decoded.Gaps.WithheldDependencyCgoDeclarations != 14 || decoded.Verdict != "pass" {
		test.Errorf("under --json the summary is %+v, want both gaps and a pass:\n%s", decoded, summaryLine)
	}
}

// A Go run that withheld nothing, as on macOS, says nothing of it: no line in the account, no marker in the
// footer, and neither field in `--json`'s gaps.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestEngineRunSaysNothingWhenAGoRunWithheldNothing(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	nothing := goStreamWithheld(test, `"withheldInDependencyCgo":[],"withheldDependencyCgoDeclarations":0`)

	verbose, _ := renderGoStreamAs(test, outputVerbose, nothing)
	requireLines(test, verbose, "types: 0 diagnostics over 3 files in 240ms\n")
	forbidLines(test, verbose, "withheld", "cgo")
	human, _ := renderGoStreamAs(test, outputHuman, nothing)
	forbidLines(test, human, "withheld", "cgo")

	jsonOutput, _ := renderGoStreamAs(test, outputJson, nothing)
	forbidLines(test, summaryJsonOf(test, jsonOutput), "withheldInDependencyCgo", "withheldDependencyCgoDeclarations")
}

// swiftStreamSeeded is the clean Swift stream with these fields added to its types record.
func swiftStreamSeeded(test *testing.T, seedFields string) []string {
	test.Helper()
	lines := contractFixture(test, "Clean.jsonl")
	for index, line := range lines {
		if strings.HasPrefix(line, `{"kind":"types"`) {
			lines[index] = strings.TrimSuffix(line, "}") + "," + seedFields + "}"
			return lines
		}
	}
	test.Fatal("Clean.jsonl holds no types record")
	return nil
}

// renderSwiftStreamAs renders a Swift stream under one output mode from a fresh summary, and gives the output with
// the summary the run left.
func renderSwiftStreamAs(test *testing.T, mode outputMode, lines []string) (string, runSummary) {
	test.Helper()
	activeOutput, activeSummary = outputSettings{Mode: mode}, runSummary{}
	output, exitCode, renderRecordsInError := renderRecordsIn(test, swiftLanguage, engineModeCheck, lines, 0, nil)
	if renderRecordsInError != nil || exitCode != 0 {
		test.Fatalf("exit %d, err %v\n%s", exitCode, renderRecordsInError, output)
	}
	return output, activeSummary
}

// A Swift run whose scripts' check took the toolchain's SDK index units from a seed says so where #f6pqraj ruled:
// one line in the account `--verbose` prints, `swift.sdkIndexSeed` in `--json`'s summary, nothing in the default
// view. A miss is said as plainly, and a run with no seed field says nothing of one.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestEngineRunNamesTheSdkIndexSeed(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	hit := swiftStreamSeeded(test, `"sdkIndexSeed":{"key":"81593ad4","hit":true}`)

	verbose, summary := renderSwiftStreamAs(test, outputVerbose, hit)
	requireLines(test, verbose, "  types: SDK index units from the toolchain seed 81593ad4\n")
	if want := (&swiftSummary{SdkIndexSeed: &sdkIndexSeedUse{Key: "81593ad4", Hit: true}}); !reflect.DeepEqual(
		summary.Swift,
		want,
	) {
		test.Errorf("the summary's swift is %+v, want %+v", summary.Swift, want)
	}
	human, _ := renderSwiftStreamAs(test, outputHuman, hit)
	forbidLines(test, human, "seed", "81593ad4")

	jsonOutput, _ := renderSwiftStreamAs(test, outputJson, hit)
	var decoded struct {
		Swift struct {
			SdkIndexSeed sdkIndexSeedUse `json:"sdkIndexSeed"`
		} `json:"swift"`
	}
	summaryLine := summaryJsonOf(test, jsonOutput)
	if decodeError := json.Unmarshal([]byte(summaryLine), &decoded); decodeError != nil {
		test.Fatalf("the summary line does not decode: %v\n%s", decodeError, summaryLine)
	}
	if decoded.Swift.SdkIndexSeed != (sdkIndexSeedUse{Key: "81593ad4", Hit: true}) {
		test.Errorf("under --json the seed is %+v:\n%s", decoded.Swift.SdkIndexSeed, summaryLine)
	}

	missed, _ := renderSwiftStreamAs(
		test,
		outputVerbose,
		swiftStreamSeeded(test, `"sdkIndexSeed":{"key":"81593ad4","hit":false}`),
	)
	requireLines(
		test,
		missed,
		"  types: no toolchain seed 81593ad4 held the SDK's index units, so they were indexed afresh\n",
	)

	plain := contractFixture(test, "Clean.jsonl")
	plainVerbose, plainSummary := renderSwiftStreamAs(test, outputVerbose, plain)
	forbidLines(test, plainVerbose, "seed")
	plainJson, _ := renderSwiftStreamAs(test, outputJson, plain)
	forbidLines(test, summaryJsonOf(test, plainJson), `"swift"`)
	if plainSummary.Swift != nil {
		test.Errorf("a run with no seed field left %+v", plainSummary.Swift)
	}
}

// The count line says each part only when there is some, in the singular where there is one.
func TestWithheldCgoSentenceCountsEachPartItHas(test *testing.T) {
	test.Parallel()
	net := withheldCgoFile{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 1}
	user := withheldCgoFile{Package: "os/user", File: "/goroot/src/os/user/cgo_lookup_unix.go", Errors: 2}
	for _, check := range []struct {
		files        []withheldCgoFile
		declarations int
		want         string
	}{
		{nil, 0, ""},
		{[]withheldCgoFile{}, 0, ""},
		{[]withheldCgoFile{net}, 1, "withheld 1 type error in 1 file of dependencies with cgo, and 1 of their " +
			"declarations was given without bodies or initial values, since no cgo tool ran"},
		{[]withheldCgoFile{net, user}, 0, "withheld 3 type errors in 2 files of dependencies with cgo, since no cgo " +
			"tool ran"},
		{nil, 1, "1 declaration of dependencies with cgo was given without bodies or initial values, since no cgo " +
			"tool ran"},
		{nil, 14, "14 declarations of dependencies with cgo were given without bodies or initial values, since no " +
			"cgo tool ran"},
	} {
		if got := withheldCgoSentence(check.files, check.declarations); got != check.want {
			test.Errorf("%v and %d declarations:\n got  %q\n want %q", check.files, check.declarations, got, check.want)
		}
	}
}

// TestEngineRunLabelsAGoCompilerFindingGo holds that a compiler finding with no group of its own carries
// the Go compiler's label where a rule goes, as a Swift one carries swiftc's.
// Not parallel: it swaps the package-level activeOutput and activeSummary, and acceptFinding counts into
// activeSummary
func TestEngineRunLabelsAGoCompilerFindingGo(test *testing.T) {
	saved, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = saved, savedSummary }()
	activeOutput, activeSummary = outputSettings{Mode: outputHuman}, runSummary{}

	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, engineModeCheck, goLanguage("module"), "", time.Now())
	record := &engineFindingRecord{Source: "compiler", File: "internal/thing/thing.go", Line: 3, Column: 7,
		Severity: "error", Message: "undefined: missing"}
	if acceptFindingError := run.acceptFinding(record); acceptFindingError != nil {
		test.Fatal(acceptFindingError)
	}
	flushStream(test, stream)
	if want := "internal/thing/thing.go:3:7 error go undefined: missing\n"; out.String() != want {
		test.Errorf("finding:\n got  %q\n want %q", out.String(), want)
	}
}

// TestEngineRunVersionInGoWords holds the Go engine's `--version` lines: its own provenance line, the
// modified-tree warning when its provenance says so, and never a source-commit line, since nothing builds
// the Go engine from a commit on the front door's behalf.
func TestEngineRunVersionInGoWords(test *testing.T) {
	test.Parallel()
	provenance := contractFixture(test, "GoClean.jsonl")[0]

	render := func(provenanceLine string, sourceCommit string) string {
		var out bytes.Buffer
		stream := bufio.NewWriter(&out)
		run := newEngineRun(stream, engineModeVersion, goLanguage("module"), "", time.Now())
		run.engineSourceCommit = sourceCommit
		if acceptError := run.accept([]byte(provenanceLine)); acceptError != nil {
			test.Fatal(acceptError)
		}
		if _, finishError := run.finish(0, "exited 0"); finishError != nil {
			test.Fatal(finishError)
		}
		flushStream(test, stream)
		return out.String()
	}

	clean := render(provenance, "a97349d9534c52c831ec0835fe7ceba880637fd1")
	requireLines(
		test,
		clean,
		"\n  swift contract: 3\n  go standard library table: none, a local build, so a run computes it\n"+
			"go engine: cohere-go 0.1.0 (commit dev, go1.27.0 darwin/arm64, contract 3)\n",
	)
	forbidLines(test, clean, "modified tree", "identical to commit", "swift engine", "Swift", "swift-syntax")

	modified := render(strings.Replace(provenance, `"sourceTreeModified":false`, `"sourceTreeModified":true`, 1), "")
	requireLines(test, modified,
		"go engine: cohere-go 0.1.0 (commit dev, go1.27.0 darwin/arm64, contract 3)\n",
		"  the Go engine was built from a modified tree, so no commit reproduces it\n",
	)
}

// TestASwiftRunUnderNoCacheSaysItsCacheWasOff: a Swift run under `--no-cache` says so in its summary's
// `cache.off` and in the verbose account, as a TypeScript run does, and a run with its cache on says neither.
// Every Swift summary once said the cache was on under `--no-cache`, because only the TypeScript pipeline set it.
// Not parallel: it sets the package-level cacheOff and activeOutput, which every run reads.
func TestASwiftRunUnderNoCacheSaysItsCacheWasOff(test *testing.T) {
	savedCacheOff, savedOutput := cacheOff, activeOutput
	defer func() { cacheOff, activeOutput = savedCacheOff, savedOutput }()
	for _, off := range []bool{true, false} {
		cacheOff = off
		activeOutput = outputSettings{Mode: outputJson}
		output, _, renderRecordsError := renderRecords(test, engineModeCheck, contractFixture(test, "Clean.jsonl"), 0)
		if renderRecordsError != nil {
			test.Fatalf("rendering under cacheOff %t: %v", off, renderRecordsError)
		}
		summaryLine := ""
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, `"kind":"summary"`) {
				summaryLine = line
			}
		}
		var summary struct {
			Cache struct {
				Off bool `json:"off"`
			} `json:"cache"`
		}
		if decodeError := json.Unmarshal([]byte(summaryLine), &summary); decodeError != nil {
			test.Fatalf("no summary record under cacheOff %t: %v\n%s", off, decodeError, output)
		}
		if summary.Cache.Off != off {
			test.Errorf("the summary's cache.off is %t under cacheOff %t", summary.Cache.Off, off)
		}

		activeOutput = outputSettings{Mode: outputVerbose}
		output, _, renderRecordsError = renderRecords(test, engineModeCheck, contractFixture(test, "Clean.jsonl"), 0)
		if renderRecordsError != nil {
			test.Fatalf("rendering verbose under cacheOff %t: %v", off, renderRecordsError)
		}
		said := strings.Contains(
			output,
			"  cache: off, by --no-cache: nothing was read from or written to this package's cache",
		)
		if said != off {
			test.Errorf("the account said the cache was off %t under cacheOff %t:\n%s", said, off, output)
		}
	}
}
