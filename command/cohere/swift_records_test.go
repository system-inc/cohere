package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// contractFixture reads one of the fixtures swift/Contract.md defines, from the same files the Swift
// encoder's tests read, so neither side can drift without the other's test failing.
func contractFixture(t *testing.T, name string) []string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "swift", "Contract", name))
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(contents), "\n"), "\n") {
		lines = append(lines, line)
	}
	if len(lines) < 2 {
		t.Fatalf("%s holds %d records, which cannot be a contract fixture", name, len(lines))
	}
	return lines
}

// renderRecords feeds records through a fresh renderer and ends the run the way the engine would.
func renderRecords(t *testing.T, mode swiftMode, lines []string, engineExit int) (string, int, error) {
	t.Helper()
	var out bytes.Buffer
	run := newSwiftRun(&out, mode, "", time.Now())
	for index, line := range lines {
		if err := run.accept([]byte(line)); err != nil {
			run.writeUnfinished("the Swift engine broke its contract")
			return out.String(), 1, &recordError{index: index, err: err}
		}
	}
	exitCode, err := run.finish(engineExit, "exited "+string(rune('0'+engineExit)))
	return out.String(), exitCode, err
}

// recordError says which record a refusal came at, so a failing case names the line that tripped.
type recordError struct {
	index int
	err   error
}

func (e *recordError) Error() string { return e.err.Error() }

func requireLines(t *testing.T, output string, wanted ...string) {
	t.Helper()
	for _, line := range wanted {
		if !strings.Contains(output, line) {
			t.Errorf("missing %q from:\n%s", line, output)
		}
	}
}

func forbidLines(t *testing.T, output string, unwanted ...string) {
	t.Helper()
	for _, line := range unwanted {
		if strings.Contains(output, line) {
			t.Errorf("printed %q, which it must not:\n%s", line, output)
		}
	}
}

func TestSwiftRunRendersEachContractFixture(t *testing.T) {
	t.Run("Clean", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "Clean.jsonl"), 0)
		if err != nil || exitCode != 0 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
		}
		requireLines(t, output,
			"package described in 412ms — 3 Swift files in the package, 2 of them ours\n",
			"fix: 0 of 2 files rewritten, 0 fixes applied, 0 refused\n",
			"format scope: changed files (working tree against HEAD): 0\n",
			"types: 0 diagnostics over 2 files in 820ms\n",
			"lint: 0 findings — 4 rules over 2 files, 512 nodes visited, walked by the fix phase (nothing was rewritten, so its findings still hold)\n",
			"  note: 4 rules watched files and reported nothing",
			"  config: no CohereSettings.json beside Package.swift, so every house rule ran at error",
			"  not checked: /project/Sources/Example/Generated.swift (marked // @generated)\n",
			"phases: fix ran in 31ms · types ran in 820ms · lint reused the fix phase's walk (nothing was rewritten) · unused skipped",
			"— package 412ms",
		)
		forbidLines(t, output, "did not check everything", "modified tree", "in scope (")
	})

	t.Run("Findings", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "Findings.jsonl"), 1)
		if err != nil || exitCode != 1 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
		}
		requireLines(t, output,
			"fix: 1 of 2 files rewritten, 1 fixes applied, 0 refused, 1 reformatted\n",
			"/project/Sources/Example/Warning.swift:2:9 - warning: variable 'neverMutated' was never mutated; consider changing to 'let' constant [#VariableNeverMutated]\n",
			// The message's newline is collapsed, so the rule tag stays on the line with the position.
			"/project/Sources/Example/ByteRing.swift:159:61 - A force unwrap crashes the process when the value is nil. Say what happens on nil with guard let or if let. [cohere-swift/no-force-unwrap/forceUnwrap]\n",
			"  note: rule cohere-swift/no-force-cast listened to no files",
			"  this binary was built from a modified tree, so no commit reproduces these findings\n",
		)
		forbidLines(t, output, "did not check everything")
	})

	t.Run("TypesBail", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "TypesBail.jsonl"), 1)
		if err != nil || exitCode != 1 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
		}
		requireLines(t, output,
			"/project/Sources/Example/Broken.swift:3:23 - error: cannot convert value of type 'String' to specified type 'Int'\n",
			"fix skipped (--no-fix)",
			"lint did not run (types bailed: 1 type errors — lint findings against wrong semantics are noise)",
			"  this run did not check everything — the phases above say what was not checked\n",
		)
		// A compiler finding with no group has no bracket.
		forbidLines(t, output, "Int' [#")
	})

	// Contract 2: the file the engine could not read is named with the excluded files, and the run is
	// incomplete with exit 1 though every phase ran and nothing was found.
	t.Run("Unreadable", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "Unreadable.jsonl"), 1)
		if err != nil || exitCode != 1 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
		}
		requireLines(t, output,
			"  not checked: /project/Sources/Example/Latin1.swift (could not be read: it is not valid UTF-8 or could not be opened (Foundation could not decode it as UTF-8))\n",
			"  not checked: /project/Sources/Example/Generated.swift (marked // @generated)\n",
			"  this run did not check everything — the notes above name the files nothing checked\n",
		)
	})

	t.Run("NothingChanged", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "NothingChanged.jsonl"), 0)
		if err != nil || exitCode != 0 {
			t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
		}
		requireLines(t, output,
			"  nothing changed against HEAD: 0 files checked\n",
			"— no package was described and no phase ran\n",
		)
		forbidLines(t, output, "did not check everything", "package described in")
	})

	// The contract's crash: the stream stops after the fix phase. Exit 0 or 1 without a summary is a
	// crash whatever the code says, so both are asserted.
	for _, engineExit := range []int{0, 1} {
		t.Run("CrashWithoutSummary", func(t *testing.T) {
			output, exitCode, err := renderRecords(t, swiftModeCheck, contractFixture(t, "CrashWithoutSummary.jsonl"), engineExit)
			if err == nil || exitCode != 1 {
				t.Fatalf("a stream with no summary exited %d with err %v\n%s", exitCode, err, output)
			}
			if !strings.Contains(err.Error(), "without a summary, so nothing was checked") {
				t.Errorf("the crash did not say nothing was checked: %v", err)
			}
			requireLines(t, output,
				"types did not run (the Swift engine exited",
				"  this run did not check everything",
			)
		})
	}
}

// TestSwiftRunRefusesBrokenStreams holds every refusal the contract asks for, each as a mutation of a
// real fixture. Every one must exit 1 with an error, and none may print a clean bill of health.
func TestSwiftRunRefusesBrokenStreams(t *testing.T) {
	findings := contractFixture(t, "Findings.jsonl")
	clean := contractFixture(t, "Clean.jsonl")
	bail := contractFixture(t, "TypesBail.jsonl")
	unreadable := contractFixture(t, "Unreadable.jsonl")
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
		{"a line that is not a record", replace(findings, 4, "warning: the build printed to stdout"), 1, "not a record"},
		{"a record of unknown kind", replace(findings, 4, `{"kind":"telemetry"}`), 1, "unknown kind"},
		{"provenance not first", without(findings, 0), 1, "provenance is always first"},
		{"a contract the front door does not speak", replace(findings, 0, strings.Replace(findings[0], `"contract":2`, `"contract":3`, 1)), 1, "contract 3"},
		{"a provenance with no contract", replace(findings, 0, strings.Replace(findings[0], `"contract":2,`, "", 1)), 1, "contract none"},
		{"a finding the summary does not count", replace(findings, last, strings.Replace(findings[last], `"findings":2`, `"findings":1`, 1)), 1, "counts 1 findings and 2"},
		{"a types record that disagrees with its findings", replace(findings, 5, strings.Replace(findings[5], `"diagnostics":1`, `"diagnostics":0`, 1)), 1, "types record counts 0"},
		{"a lint record that disagrees with its findings", replace(findings, 8, strings.Replace(findings[8], `"findings":1`, `"findings":3`, 1)), 1, "lint record counts 3"},
		{"a missing phase record", without(findings, 9), 1, "expects lint"},
		{"phases out of order", replace(clean, 3, `{"kind":"phase","name":"lint","outcome":"ran","elapsedMilliseconds":1,"detail":""}`), 0, "expects fix"},
		{"an outcome the pipeline does not have", replace(clean, 3, strings.Replace(clean[3], `"ran"`, `"partial"`, 1)), 0, "not one of"},
		{"a record after the summary", append(append([]string(nil), clean...), clean[1]), 0, "after the summary"},
		// The dangerous direction: lint was cut off and the summary claims the run was complete.
		{"a summary claiming complete over a bailed lint", replace(bail, len(bail)-1, strings.Replace(bail[len(bail)-1], `"complete":false`, `"complete":true`, 1)), 1, "calls the run complete"},
		{"a summary whose exit code disagrees with its own findings", replace(findings, last, strings.Replace(findings[last], `"exitCode":1`, `"exitCode":0`, 1)), 0, "summary says exit 0"},
		{"an engine whose exit disagrees with its summary", findings, 0, "said it would exit 1"},
		{"a summary missing its fields", replace(findings, last, `{"kind":"summary"}`), 1, "missing findings"},
		// Contract 2's unreadable record: a summary may not call the run complete over one, and it has one place.
		{"a summary claiming complete over an unreadable file", replace(unreadable, len(unreadable)-1, `{"kind":"summary","findings":0,"complete":true,"nothingToCheck":"","exitCode":0}`), 0, "calls the run complete"},
		{"an unreadable record after a phase", append(append(append([]string(nil), unreadable[:5]...), unreadable[2]), unreadable[5:]...), 1, "after phase fix"},
		{"an unreadable record before the project", append([]string{unreadable[0], unreadable[2]}, unreadable[1:]...), 1, "before the project record"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, exitCode, err := renderRecords(t, swiftModeCheck, testCase.lines, testCase.engineExit)
			if err == nil || exitCode != 1 {
				t.Fatalf("accepted a broken stream: exit %d, err %v\n%s", exitCode, err, output)
			}
			if !strings.Contains(err.Error(), testCase.reason) {
				t.Errorf("refused for the wrong reason: %v, want one naming %q", err, testCase.reason)
			}
		})
	}
}

// TestSwiftRunGroupsExcludedFiles holds both sides of the limit: a reason with many files is one
// counted line, and a reason with few still names each file.
func TestSwiftRunGroupsExcludedFiles(t *testing.T) {
	clean := contractFixture(t, "Clean.jsonl")
	vendored := []string{}
	for index := range excludedNamedLimit + 1 {
		vendored = append(vendored, `{"file":"/project/Vendor/F`+string(rune('a'+index))+`.swift","reason":"vendored under Vendor/"}`)
	}
	clean[1] = strings.Replace(clean[1], `"excluded":[`, `"excluded":[`+strings.Join(vendored, ",")+",", 1)

	output, exitCode, err := renderRecords(t, swiftModeCheck, clean, 0)
	if err != nil || exitCode != 0 {
		t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
	}
	requireLines(t, output,
		"  not checked: 6 files (vendored under Vendor/)\n",
		"  not checked: /project/Sources/Example/Generated.swift (marked // @generated)\n",
	)
	forbidLines(t, output, "/project/Vendor/Fa.swift")
}

// TestSwiftRunBelievesAnEngineThatFellShort holds the allowed direction of the completeness check: an
// engine that says it did not check everything is believed even when its records show no gap, because
// it can see gaps no record carries, such as a file it could not read.
func TestSwiftRunBelievesAnEngineThatFellShort(t *testing.T) {
	clean := contractFixture(t, "Clean.jsonl")
	last := len(clean) - 1
	clean[last] = strings.Replace(strings.Replace(clean[last], `"complete":true`, `"complete":false`, 1), `"exitCode":0`, `"exitCode":1`, 1)

	output, exitCode, err := renderRecords(t, swiftModeCheck, clean, 1)
	if err != nil || exitCode != 1 {
		t.Fatalf("exit %d, err %v\n%s", exitCode, err, output)
	}
	requireLines(t, output, "  this run did not check everything — the Swift engine reported that it fell short without a record saying where\n")
}

// TestSwiftRunListingModes holds the two runs that end without a summary on purpose.
func TestSwiftRunListingModes(t *testing.T) {
	provenance := contractFixture(t, "Clean.jsonl")[0]

	t.Run("version", func(t *testing.T) {
		output, exitCode, err := renderRecords(t, swiftModeVersion, []string{provenance}, 0)
		if err != nil || exitCode != 0 {
			t.Fatalf("exit %d, err %v", exitCode, err)
		}
		requireLines(t, output, "swift engine: cohere-swift 0.1.0 (commit dev, swiftlang-6.4.0.34.1, swift-syntax 604.0.0, swift-format 604.0.0)\n")
	})

	t.Run("rules, sorted", func(t *testing.T) {
		lines := []string{
			provenance,
			`{"kind":"rule","name":"cohere-swift/no-try-bang","severity":"error"}`,
			`{"kind":"rule","name":"cohere-swift/no-force-unwrap","severity":"warning"}`,
		}
		output, exitCode, err := renderRecords(t, swiftModeRules, lines, 0)
		if err != nil || exitCode != 0 {
			t.Fatalf("exit %d, err %v", exitCode, err)
		}
		if output != "cohere-swift/no-force-unwrap\ncohere-swift/no-try-bang\n" {
			t.Errorf("rules printed as %q", output)
		}
		enabled, _, err := renderRecords(t, swiftModeRulesEnabled, lines, 0)
		if err != nil || enabled != "cohere-swift/no-force-unwrap\twarning\ncohere-swift/no-try-bang\terror\n" {
			t.Errorf("rules-enabled printed as %q (err %v)", enabled, err)
		}
	})

	t.Run("a finding in a listing is refused", func(t *testing.T) {
		findings := contractFixture(t, "Findings.jsonl")
		_, exitCode, err := renderRecords(t, swiftModeRules, []string{provenance, findings[4]}, 0)
		if err == nil || exitCode != 1 {
			t.Fatalf("a finding in a rules listing was accepted")
		}
	})

	t.Run("an engine that could not run", func(t *testing.T) {
		_, exitCode, err := renderRecords(t, swiftModeCheck, []string{provenance}, 2)
		if err == nil || exitCode != 1 || !strings.Contains(err.Error(), "did not run, so nothing was checked") {
			t.Fatalf("exit 2 was rendered as exit %d, err %v", exitCode, err)
		}
	})
}

func TestSwiftEngineArguments(t *testing.T) {
	location := projectLocation{
		Root:               "/work/macos",
		Engine:             engineSwift,
		LintConfigFileName: "/work/macos/sub/Local.json",
		ArgumentBase:       "/work/macos/sub",
	}
	values := map[string]string{"no-fix": "true", "changed": "false", "fix-passes": "4", "rules": "true"}
	value := func(name string) string { return values[name] }

	arguments, mode, err := swiftEngineArguments(location,
		map[string]bool{"no-fix": true, "changed": true, "fix-passes": true, "lint-config": true},
		value, []string{"Thing.swift", "/abs/Other.swift"})
	if err != nil {
		t.Fatal(err)
	}
	want := "--contract 2 --root /work/macos --no-fix --lint-config /work/macos/sub/Local.json --fix-passes 4 /work/macos/sub/Thing.swift /abs/Other.swift"
	if strings.Join(arguments, " ") != want || mode != swiftModeCheck {
		// `--changed=false` was typed and is false, so it is not forwarded as a switch.
		t.Errorf("arguments %q mode %s, want %q", strings.Join(arguments, " "), mode, want)
	}

	_, mode, _ = swiftEngineArguments(location, map[string]bool{"rules": true}, value, nil)
	if mode != swiftModeRules {
		t.Errorf("--rules ran in mode %s", mode)
	}

	if _, _, err := swiftEngineArguments(location, map[string]bool{"explain": true}, value, nil); err == nil || !strings.Contains(err.Error(), "--explain") {
		t.Errorf("--explain was not refused by name: %v", err)
	}
}
