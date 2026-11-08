package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Kirk's rulings on what an ignore leaves checked (#cxeq6jw): a plain entry means never checked at all, its files'
// type errors dropped and their count in no total; an entry with `"check": "types"` keeps the type check, its
// files' type errors reported, never linted or formatted, and counted as types-only; where both match a file the
// explicit one keeps types. cohere's defaults, testdata and Vendor, are listed as ignores and act on no file a
// tsconfig includes.

// checkIgnoreSettings ignores plain/ outright and keeps the type check for typed/ and for plain/kept/, which the
// plain entry matches too.
const checkIgnoreSettings = `{"rules": {"no-debugger": "error"}, "ignorePatterns": [` +
	`{"pattern": "plain/**", "reason": "Exports a test reads as they are.", "kind": "input"}, ` +
	`{"pattern": "typed/**", "reason": "A vendored client.", "kind": "thirdParty", "check": "types"}, ` +
	`{"pattern": "plain/kept/**", "reason": "Exports whose types still matter.", "check": "types"}]}`

// checkIgnoreFixture is a project under checkIgnoreSettings holding index.ts and files, which name a file in each
// of plain/, typed/ and plain/kept/ for a test to plant in.
func checkIgnoreFixture(test *testing.T, files map[string]string) string {
	test.Helper()
	root := test.TempDir()
	tree := map[string]string{
		"tsconfig.json":       incrementalFixtureConfiguration,
		"CohereSettings.json": checkIgnoreSettings,
		".gitignore":          "tsconfig.tsbuildinfo\n",
		"index.ts":            "export const value = 1;\n",
	}
	for name, contents := range files {
		tree[name] = contents
	}
	writeTree(test, root, tree)
	return root
}

// checkIgnoreSummary is the summary line of a `--json` run's output.
func checkIgnoreSummary(test *testing.T, output string) summaryJson {
	test.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &kind) != nil || kind.Kind != "summary" {
			continue
		}
		var summary summaryJson
		if decodeError := json.Unmarshal([]byte(line), &summary); decodeError != nil {
			test.Fatal(decodeError)
		}
		return summary
	}
	test.Fatalf("no summary line:\n%s", output)
	return summaryJson{}
}

func TestAnIgnoreKeepsTheTypeCheckOnlyWhereAnEntryNamesIt(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	broken := map[string]string{
		"plain/Broken.ts":      "export const plainBroken: number = \"text\";\n",
		"typed/Broken.ts":      "export const typedBroken: number = \"text\";\n",
		"plain/kept/Broken.ts": "export const keptBroken: number = \"text\";\n",
	}

	// Each phase that reports type errors: the whole run, which reads the settings to lint, and `--types`, which
	// reads them only for this.
	for _, arguments := range [][]string{{"--no-fix", "--no-format", "--no-cache"}, {"--types", "--no-cache"}} {
		test.Run("type errors under "+strings.Join(arguments, " "), func(test *testing.T) {
			test.Parallel()
			root := checkIgnoreFixture(test, broken)
			output, code := runCohere(test, binary, root, arguments...)
			if code == 0 {
				test.Errorf("exit 0 over two type errors an entry keeping types matched:\n%s", output)
			}
			for _, kept := range []string{"typed/Broken.ts", "plain/kept/Broken.ts"} {
				if !strings.Contains(output, filepath.Join(root, kept)+":1:14 - error TS2322") {
					test.Errorf("the type error in %s, which an entry keeping types matched, was not reported:\n%s",
						kept, output)
				}
			}
			if strings.Contains(output, "plainBroken") ||
				strings.Contains(output, filepath.Join(root, "plain/Broken.ts")) {
				test.Errorf("the type error in plain/Broken.ts, which only a plain entry matched, was reported:\n%s",
					output)
			}
			if count := strings.Count(output, "error TS2322"); count != 2 {
				test.Errorf("%d type errors reported, want 2:\n%s", count, output)
			}
			if !strings.Contains(output, "types: 2 diagnostics over 3 files") {
				test.Errorf("the types line does not count the three files it reports on:\n%s", output)
			}
			// index.ts alone is checked: plain/Broken.ts is in no count, and the two kept are types-only.
			if !strings.Contains(output, "1 checked • 2 types-only") {
				test.Errorf("the footer does not say 1 checked and 2 types-only:\n%s", output)
			}

			output, _ = runCohere(test, binary, root, append(arguments, "--json")...)
			summary := checkIgnoreSummary(test, output)
			if summary.FilesInScope != 1 || summary.Checked != 1 || summary.TypesOnly != 2 || summary.TypeErrors != 2 {
				test.Errorf("the summary reads %d in scope, %d checked, %d types-only, %d type errors, "+
					"want 1, 1, 2, 2:\n%s",
					summary.FilesInScope, summary.Checked, summary.TypesOnly, summary.TypeErrors, output)
			}
		})
	}

	// No type error, so lint runs: a finding in index.ts says it did, and none in the ignored files says an entry
	// keeping types keeps lint out as a plain one does.
	test.Run("never linted", func(test *testing.T) {
		test.Parallel()
		debugged := func(name string) string {
			return "export function " + name + "(): number {\n  debugger;\n  return 1;\n}\n"
		}
		root := checkIgnoreFixture(test, map[string]string{
			"index.ts":               debugged("value"),
			"plain/Debugged.ts":      debugged("plainDebugged"),
			"typed/Debugged.ts":      debugged("typedDebugged"),
			"plain/kept/Debugged.ts": debugged("keptDebugged"),
		})
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code == 0 {
			test.Errorf("exit 0 over a debugger statement in index.ts:\n%s", output)
		}
		if count := strings.Count(output, "[no-debugger/"); count != 1 {
			test.Errorf("no-debugger reported %d times, want once, in index.ts:\n%s", count, output)
		}
		if !strings.Contains(output, filepath.Join(root, "index.ts")+":2:3") {
			test.Errorf("no-debugger did not report index.ts, so lint did not run and this proves nothing:\n%s", output)
		}
		if !strings.Contains(output, "1 checked • 2 types-only") {
			test.Errorf("lint's footer does not say 1 checked and 2 types-only:\n%s", output)
		}
		if !strings.Contains(output, "3 files excluded by ignorePatterns, 2 of them still type-checked") {
			test.Errorf("the coverage line does not say which excluded files are still type-checked:\n%s", output)
		}
	})

	// A writing format run formats index.ts and leaves every ignored file as it was.
	test.Run("never formatted", func(test *testing.T) {
		test.Parallel()
		ugly := "export const ugly   =   1;\n"
		root := checkIgnoreFixture(test, map[string]string{
			"index.ts":           ugly,
			"plain/Ugly.ts":      ugly,
			"typed/Ugly.ts":      ugly,
			"plain/kept/Ugly.ts": ugly,
		})
		if output, code := runCohere(test, binary, root, "--format-only", "--no-cache"); code != 0 {
			test.Fatalf("the writing format run exited %d:\n%s", code, output)
		}
		if got := readForTest(test, filepath.Join(root, "index.ts")); got != "export const ugly = 1;\n" {
			test.Fatalf("index.ts was not formatted, so this proves nothing:\n%s", got)
		}
		for _, name := range []string{"plain/Ugly.ts", "typed/Ugly.ts", "plain/kept/Ugly.ts"} {
			if got := readForTest(test, filepath.Join(root, name)); got != ugly {
				test.Errorf("%s, which an ignore matched, was formatted:\n%s", name, got)
			}
		}
	})
}

// cohere's defaults are listed as ignores, with their kind and reason, and act on no file a tsconfig includes:
// a file under testdata or Vendor is type-checked and linted as any other, and a default that matched nothing is
// no finding.
func TestTheDefaultIgnoresAreListedAndActOnNoProgramFile(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)

	test.Run("listed, and never a dead ignore", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":       incrementalFixtureConfiguration,
			"CohereSettings.json": `{"rules": {}}`,
			".gitignore":          "tsconfig.tsbuildinfo\n",
			"index.ts":            "export const value = 1;\n",
		})
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 0 {
			test.Errorf("exit %d over defaults that matched nothing, want 0:\n%s", code, output)
		}
		if strings.Contains(output, unusedIgnoreRule) {
			test.Errorf("a default that matched nothing was a finding:\n%s", output)
		}
		for _, want := range []string{
			`ignore: "**/testdata/**" in cohere's defaults, input, matched 0 paths: Go's test fixtures`,
			`ignore: "**/Vendor/**" in cohere's defaults, thirdParty, matched 0 paths: Somebody else's code`,
		} {
			if !strings.Contains(output, want) {
				test.Errorf("--verbose did not list %q:\n%s", want, output)
			}
		}

		output, code = runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache", "--json")
		if code != 0 {
			test.Errorf("exit %d under --json, want 0:\n%s", code, output)
		}
		summary := checkIgnoreSummary(test, output)
		want := defaultIgnoreAccounts(nil)
		for index := range want {
			want[index].shownFile = ""
		}
		if !reflect.DeepEqual(summary.IgnorePatterns, want) {
			test.Errorf("the summary's ignorePatterns are\n  %+v\nwant\n  %+v", summary.IgnorePatterns, want)
		}
		if summary.Findings != 0 {
			test.Errorf("the summary counts %d findings, want 0:\n%s", summary.Findings, output)
		}
	})

	for _, directory := range []string{"source/testdata", "source/Vendor"} {
		test.Run("a file under "+directory+" is type-checked and linted", func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			writeTree(test, root, map[string]string{
				"tsconfig.json":                  incrementalFixtureConfiguration,
				"CohereSettings.json":            `{"rules": {"no-debugger": "error"}}`,
				".gitignore":                     "tsconfig.tsbuildinfo\n",
				"index.ts":                       "export const value = 1;\n",
				directory + "/Broken.ts":         "export const broken: number = \"text\";\n",
				directory + "/other/Debugged.ts": "export function debugged(): number {\n  debugger;\n  return 1;\n}\n",
			})
			output, _ := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
			if !strings.Contains(output, filepath.Join(root, directory, "Broken.ts")+":1:14 - error TS2322") {
				test.Errorf("the type error under %s was not reported:\n%s", directory, output)
			}
			if !strings.Contains(output, "3 checked") {
				test.Errorf("the footer does not count the files under %s as checked:\n%s", directory, output)
			}
			kind := "input"
			if strings.HasSuffix(directory, "Vendor") {
				kind = "thirdParty"
			}
			if !strings.Contains(output, `in cohere's defaults, `+kind+`, matched 1 path:`) {
				test.Errorf("the default for %s did not count the directory:\n%s", directory, output)
			}

			// The type error out of the way, lint runs and reports the file under the default too.
			writeTree(test, root, map[string]string{directory + "/Broken.ts": "export const broken = 1;\n"})
			output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
			if code == 0 {
				test.Errorf("exit 0 over a debugger statement under %s:\n%s", directory, output)
			}
			if !strings.Contains(output, filepath.Join(root, directory, "other", "Debugged.ts")+":2:3") {
				test.Errorf("no-debugger did not report the file under %s:\n%s", directory, output)
			}
			if strings.Contains(output, unusedIgnoreRule) {
				test.Errorf("a default was a finding:\n%s", output)
			}
		})
	}
}
