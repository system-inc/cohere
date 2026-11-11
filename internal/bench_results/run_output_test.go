package bench_results

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The reader reads real runs. Each file in testdata/run-output is a `cohere --json` run's whole log,
// stdout and stderr together as quiet_machine.sh keeps it, captured from cohere at 91cf8e39 on a project of
// three files: a cold run under --no-cache, a warm run that took every file from the cache, an edit run
// after a comment was added to one file, and a run with a debugger statement and an unused constant in a
// fourth, so a count of zero has been seen to read nonzero (#zrgrk14).
func TestRunOutputReadsRealRuns(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]struct {
		cache       string
		hasFindings bool
	}{
		"cold.txt":     {cache: "off"},
		"warm.txt":     {cache: "files 3/3"},
		"edit.txt":     {cache: "files "},
		"findings.txt": {cache: "", hasFindings: true},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			output, readError := os.ReadFile(filepath.Join("testdata", "run-output", name))
			if readError != nil {
				test.Fatal(readError)
			}
			run, parseRunOutputError := ParseRunOutput(output)
			if parseRunOutputError != nil {
				test.Fatal(parseRunOutputError)
			}
			if run.EngineSeconds <= 0 {
				test.Errorf("engine seconds read %g", run.EngineSeconds)
			}
			if !strings.HasPrefix(run.Cache, want.cache) {
				test.Errorf("cache read %q, want %q", run.Cache, want.cache)
			}
			if want.cache == "files " {
				edited := Run{Mode: ModeEdit, Cache: run.Cache}
				if cacheFitsModeError := cacheFitsMode(edited); cacheFitsModeError != nil {
					test.Errorf("a real edit run's cache does not fit an edit: %v", cacheFitsModeError)
				}
			}
			if (run.Findings > 0) != want.hasFindings {
				test.Errorf("findings read %d", run.Findings)
			}
		})
	}
}

// A run that did less than a developer's is refused, never timed (#7z52pqf). Both logs are real. On cohere's
// root, a benchmark copy with no git: the Go engine refused to run and the Swift engine read no input, and
// only the format project between them checked anything, so a reader of the last summary alone saw the
// Swift error but would have timed a pass had the projects printed in another order. On ahra, a copy without
// the route types Next's dev server generates: 161 type errors, fixes held, and no rule run. And the cohere
// root's log again with the reasons taken out of its failed projects (`error` and `phasesNotRun`), so a
// refusal that forgets to say why is still never timed. And ahra's October tree on today's dependencies under
// main's engine, whose 2 type errors kept lint from running though no gap says so.
func TestRunOutputRefusesARunThatCheckedLess(test *testing.T) {
	test.Parallel()
	for name, wantReason := range map[string]string{
		"no-git-cohere-root.txt":         `the project "." did less than a developer's run: The Go engine did not run`,
		"missing-generated-ahra.txt":     `the project "." did less than a developer's run: generated files it asks for are missing`,
		"failed-unnamed-cohere-root.txt": `the project "." did less than a developer's run: it failed with nothing reported`,
		"lint-not-reached-ahra.txt":      `the project "." did less than a developer's run: its lint phase was not reached`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			output, readError := os.ReadFile(filepath.Join("testdata", "run-output", name))
			if readError != nil {
				test.Fatal(readError)
			}
			run, parseRunOutputError := ParseRunOutput(output)
			if parseRunOutputError == nil {
				test.Fatalf("read %+v", run)
			}
			if !strings.Contains(parseRunOutputError.Error(), wantReason) {
				test.Fatalf("refused with %q, want it to say %q", parseRunOutputError, wantReason)
			}
		})
	}
}

// Every gap that means less was checked refuses the run, and the ones a developer's run meets as well do
// not. Each case is one project's summary with one gap set, beside a whole one, so the refusal is seen to
// come from that gap and from whichever project carries it.
func TestRunOutputRefusesEachGapThatChecksLess(test *testing.T) {
	test.Parallel()
	const whole = `{"kind":"summary","schemaVersion":1,"label":"whole","seconds":1,"filesInScope":3,"checked":3,` +
		`"gaps":{"programFiles":0,"crashedFiles":0,"ruleCrashes":0,"rulesSkippingEverything":0,` +
		`"formattingNotChecked":false,"nothingToCheck":false,"modifiedBuild":false}}`
	// The gapped project's summary, with `top` added to its fields and `gaps` to its gaps. A gap the whole
	// summary already holds is set by repeating its key, since the decoder keeps a repeated key's last value.
	summary := func(top string, gaps string) string {
		return `{"kind":"summary","schemaVersion":1,"label":"gapped","seconds":1,"filesInScope":3,"checked":3` + top +
			`,"gaps":{"programFiles":0,"crashedFiles":0,"ruleCrashes":0,"rulesSkippingEverything":0,` +
			`"formattingNotChecked":false,"nothingToCheck":false,"modifiedBuild":false` + gaps + `}}`
	}
	for name, testCase := range map[string]struct {
		line    string
		refused bool
	}{
		"an error":                 {line: summary(`,"error":"no engine"`, ""), refused: true},
		"phases not run":           {line: summary("", `,"phasesNotRun":["lint"]`), refused: true},
		"a crashed file":           {line: summary("", `,"crashedFiles":1`), refused: true},
		"a rule crash":             {line: summary("", `,"ruleCrashes":1`), refused: true},
		"nothing to check":         {line: summary("", `,"nothingToCheck":true`), refused: true},
		"unread files":             {line: summary("", `,"unread":"2 files"`), refused: true},
		"a narrowed program":       {line: summary("", `,"programFiles":9`), refused: true},
		"formatting not checked":   {line: summary("", `,"formattingNotChecked":true`), refused: true},
		"a missing generated file": {line: summary("", `,"missingGenerated":["gen.d.ts"]`), refused: true},
		"fixes held":               {line: summary("", `,"fixesHeld":true`), refused: true},
		"rules with nothing to do": {line: summary("", `,"rulesSkippingEverything":4`)},
		"a modified build":         {line: summary("", `,"modifiedBuild":true`)},
		"nested files unchecked":   {line: summary("", `,"nestedUnchecked":2`)},
		"Swift no target compiles": {
			line: summary("", `,"unchecked":[{"file":"a.swift","reason":"none","language":"Swift"}]`),
		},
		"Go only another platform builds": {
			line: summary("", `,"unchecked":[{"file":"a_windows.go","reason":"none","language":"Go"}]`),
		},
		"a dependency's cgo withheld": {line: summary("", `,"withheldDependencyCgoDeclarations":3`)},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			for _, order := range [][]string{{whole, testCase.line}, {testCase.line, whole}} {
				run, parseRunOutputError := ParseRunOutput([]byte(strings.Join(order, "\n") + "\n"))
				if testCase.refused && parseRunOutputError == nil {
					test.Fatalf("read %+v", run)
				}
				if !testCase.refused && parseRunOutputError != nil {
					test.Fatalf("refused: %v", parseRunOutputError)
				}
				if testCase.refused && !strings.Contains(parseRunOutputError.Error(), `"gapped"`) {
					test.Fatalf("refused without naming the project that carries the gap: %v", parseRunOutputError)
				}
			}
		})
	}
}

// A root of several projects is all of them, which run side by side: the longest engine time, every
// finding, and the files every project took from the cache. The longest project is printed first here, so
// a reader of the last summary alone reads 1.5s and 1 finding.
func TestRunOutputReadsEveryProjectOfARoot(test *testing.T) {
	test.Parallel()
	output := `{"kind":"summary","schemaVersion":1,"label":".","seconds":3.25,"findings":2,"typeErrors":1,"filesInScope":10,"cached":4,"gaps":{}}
{"kind":"summary","schemaVersion":1,"label":"swift","seconds":1.5,"findings":1,"filesInScope":5,"cached":5,"gaps":{}}
`
	run, parseRunOutputError := ParseRunOutput([]byte(output))
	if parseRunOutputError != nil {
		test.Fatal(parseRunOutputError)
	}
	if run.EngineSeconds != 3.25 || run.Findings != 4 || run.Cache != "files 9/15" {
		test.Fatalf("read %+v, want 3.25s, 4 findings and files 9/15", run)
	}
}

// A log the reader cannot read is refused, not read as zeroes: the human footer alone, which is what the
// first record's instrument read, and a summary of a contract version it does not know.
func TestRunOutputRefusesWhatItCannotRead(test *testing.T) {
	test.Parallel()
	for name, output := range map[string]string{
		"the footer alone":         "✓ 💎 0.1s (430 rules • 3,976 cached)\n",
		"another schema":           `{"kind":"summary","schemaVersion":2,"seconds":0.1}` + "\n",
		"findings with no summary": `{"kind":"finding","path":"/a.ts","line":1,"column":1}` + "\n",
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			if run, parseRunOutputError := ParseRunOutput([]byte(output)); parseRunOutputError == nil {
				test.Fatalf("read %+v", run)
			}
		})
	}
}

// A Go project's summary names the builds it checked, and the reader keeps them in order, the host's first, for the
// record (#ccqee0e). A summary that names none, a host-only run's or another engine's, adds none.
func TestRunOutputReadsTheBuildsAGoRunChecked(test *testing.T) {
	test.Parallel()
	summary := func(label string, builds string) string {
		return `{"kind":"summary","schemaVersion":1,"label":"` + label + `","verdict":"pass","seconds":1.5,` +
			`"phases":[{"name":"lint","outcome":"ran"}],"cache":{"off":true},"filesInScope":3,"checked":3` + builds +
			`}`
	}
	output := strings.Join([]string{
		summary(".", ""),
		summary(". (Go)", `,"builds":[{"target":"darwin/arm64 cgo","host":true},{"target":"linux/arm64 tags a,b"}]`),
	}, "\n")
	run, parseRunOutputError := ParseRunOutput([]byte(output))
	if parseRunOutputError != nil {
		test.Fatal(parseRunOutputError)
	}
	if want := []string{"darwin/arm64 cgo", "linux/arm64 tags a,b"}; !slices.Equal(run.Builds, want) {
		test.Errorf("the builds read %q, want %q", run.Builds, want)
	}
}
