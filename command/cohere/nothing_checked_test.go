package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A TypeScript run that checked nothing fails and says why, in the footer and in `--json` (#08cv2mp). Before, nothing
// to check was read from the program's file list, where a file an ignore matched stays, so a project whose every
// file an ignore matched read `0 checked`, passed, and said nothing of it.

// legacyIgnoredSettings ignores legacy/ outright, and vendorTypesSettings keeps the type check for vendor/. Each
// fixture names the one whose entry matches its files, since an entry that matches nothing is a finding.
const (
	legacyIgnoredSettings = `{"rules": {"no-debugger": "error"}, "ignorePatterns": [` +
		`{"pattern": "legacy/**", "reason": "Old code kept as it was.", "kind": "input"}]}`
	vendorTypesSettings = `{"rules": {"no-debugger": "error"}, "ignorePatterns": [` +
		`{"pattern": "vendor/**", "reason": "A vendored client.", "kind": "thirdParty", "check": "types"}]}`
	noIgnoreSettings = `{"rules": {"no-debugger": "error"}}`
)

// noFilesToCheckMarker is the footer's warning for a program with no file of its own, which passes.
const noFilesToCheckMarker = "⚠ no files to check"

// legacyIgnored is the reason the plain entry gives for the two files under legacy/.
var legacyIgnored = excludedReason{
	Reason: `matched ignorePatterns "legacy/**"`,
	Count:  2,
	Source: "CohereSettings.json",
}

func TestARunThatCheckedNothingFailsAndSaysWhy(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	legacy := map[string]string{
		"legacy/Old.ts":   "export const old = 1;\n",
		"legacy/Older.ts": "export const older = 2;\n",
	}
	withIndex := map[string]string{"index.ts": "export const value = 1;\n"}
	for name, contents := range legacy {
		withIndex[name] = contents
	}
	quiet := []string{"--no-fix", "--no-format", "--no-cache"}
	everyLegacyFileIgnored := `no files checked: 2 files excluded, ` +
		`matched ignorePatterns "legacy/**" (CohereSettings.json)`

	for _, testCase := range []struct {
		name      string
		files     map[string]string
		settings  string
		arguments []string
		// paths are named after the flags, since a flag after a path is read as another path.
		paths []string
		// yieldAll yields every .ts file to a nearer project, leaving the program none of its own.
		yieldAll bool

		// sentence is the footer's words for a run that checked nothing, which fails it, empty for a run that passes.
		sentence string
		// marker is whether the footer warns of a program with no file of its own, which passes.
		marker bool
		// excluded is what `--json` says kept the files out, filled only for a run that checked nothing.
		excluded []excludedReason
		// counted is what the footer counts of a run that checked something, which says the case is what it claims.
		counted string
		// formattingDecides is a run whose verdict is formatting's alone, which this fixture's settings would change.
		formattingDecides bool
	}{
		// Checked nothing, so it fails: the run's checking phases ran and checked no file it had.
		{name: "every file ignored", files: legacy, settings: legacyIgnoredSettings, arguments: quiet,
			sentence: everyLegacyFileIgnored, excluded: []excludedReason{legacyIgnored}},
		{name: "every file ignored, types alone", files: legacy, settings: legacyIgnoredSettings,
			arguments: append([]string{"--types"}, quiet...),
			sentence:  everyLegacyFileIgnored, excluded: []excludedReason{legacyIgnored}},
		{name: "every file ignored, lint alone", files: legacy, settings: legacyIgnoredSettings,
			arguments: append([]string{"--lint"}, quiet...),
			sentence:  everyLegacyFileIgnored, excluded: []excludedReason{legacyIgnored}},
		{name: "named paths that reach no program file", files: withIndex, settings: legacyIgnoredSettings,
			arguments: append([]string{"--types"}, quiet...), paths: []string{"NOTES.md"},
			sentence: "no files checked: 3 files excluded, outside the named paths",
			excluded: []excludedReason{{Reason: outsideNamedPaths, Count: 3}}},
		{name: "named paths that reach only ignored files", files: withIndex, settings: legacyIgnoredSettings,
			arguments: quiet, paths: []string{"legacy"},
			sentence: `no files checked: 3 files excluded, 1 outside the named paths; ` +
				`2 matched ignorePatterns "legacy/**" (CohereSettings.json)`,
			excluded: []excludedReason{{Reason: outsideNamedPaths, Count: 1}, legacyIgnored}},

		// A program with no file of its own had nothing to keep out: the warning, and a pass.
		{name: "an empty program", files: withIndex, settings: legacyIgnoredSettings,
			arguments: append([]string{"--types"}, quiet...), yieldAll: true, marker: true},

		// Checked a file, by lint or by the type check alone, so it passes.
		{name: "one checkable file", files: map[string]string{"index.ts": "export const value = 1;\n"},
			settings: noIgnoreSettings, arguments: quiet, counted: "• 1 checked"},
		{name: "every file ignored but one", files: withIndex, settings: legacyIgnoredSettings, arguments: quiet,
			counted: "• 1 checked"},
		{name: "every file under an entry keeping types",
			files:    map[string]string{"vendor/Client.ts": "export const client = 1;\n"},
			settings: vendorTypesSettings, arguments: quiet, counted: "1 types-only"},
		{name: "a named path that reaches a checked file", files: withIndex, settings: legacyIgnoredSettings,
			arguments: quiet, paths: []string{"index.ts"}, counted: "• 1 checked"},

		// Formatting alone reads the disk, not the program, so its account is its format scope, as it was.
		{name: "every file ignored, formatting alone", files: legacy, settings: legacyIgnoredSettings,
			arguments: []string{"--no-fix", "--format-only", "--no-cache"}, counted: "(3 checked)",
			formattingDecides: true},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			tree := map[string]string{
				"tsconfig.json":       incrementalFixtureConfiguration,
				"CohereSettings.json": testCase.settings,
				".gitignore":          "tsconfig.tsbuildinfo\n.cache/\n",
				"NOTES.md":            "# Notes\n",
			}
			for name, contents := range testCase.files {
				tree[name] = contents
			}
			var environment []string
			if testCase.yieldAll {
				environment = []string{projectYieldVariable + "=" + yieldEveryFile(test, root, testCase.files)}
			}
			writeTree(test, root, tree)
			wantVerdict := "pass"
			if testCase.sentence != "" {
				wantVerdict = "fail"
			}

			output, code := runCohereWithEnvironment(test, binary, root, environment,
				slices.Concat(testCase.arguments, testCase.paths)...)
			footerLine := lastLine(output)
			if failed := code != 0; failed != (testCase.sentence != "") && !testCase.formattingDecides {
				test.Errorf("exit %d over a run whose footer should say %q:\n%s", code, testCase.sentence, output)
			}
			failed := strings.HasPrefix(footerLine, "✗ ☠️ ")
			if failed != (testCase.sentence != "") && !testCase.formattingDecides {
				test.Errorf("the footer's verdict is wrong for a run that should say %q:\n%s", testCase.sentence,
					footerLine)
			}
			if testCase.sentence == "" && strings.Contains(footerLine, "no files checked") {
				test.Errorf("the footer says no files were checked of a run that checked one or had none:\n%s",
					footerLine)
			}
			if !strings.Contains(footerLine, testCase.sentence) {
				test.Errorf("the footer does not say %q:\n%s", testCase.sentence, footerLine)
			}
			if marked := strings.Contains(footerLine, noFilesToCheckMarker); marked != testCase.marker {
				test.Errorf("the footer says %q: %v, want %v:\n%s", noFilesToCheckMarker, marked, testCase.marker,
					footerLine)
			}
			if !strings.Contains(footerLine, testCase.counted) {
				test.Errorf("the footer does not say %q:\n%s", testCase.counted, footerLine)
			}

			output, _ = runCohereWithEnvironment(test, binary, root, environment,
				slices.Concat(testCase.arguments, []string{"--json"}, testCase.paths)...)
			summary := checkIgnoreSummary(test, output)
			if summary.Verdict != wantVerdict && !testCase.formattingDecides {
				test.Errorf("the summary's verdict is %q, want %q:\n%s", summary.Verdict, wantVerdict, output)
			}
			if nothing := testCase.sentence != "" || testCase.marker; summary.Gaps.NothingToCheck != nothing {
				test.Errorf("the summary's nothingToCheck is %v, want %v:\n%s", summary.Gaps.NothingToCheck, nothing,
					output)
			}
			if !reflect.DeepEqual(summary.Gaps.Excluded, testCase.excluded) {
				test.Errorf("the summary's excluded is\n  %+v\nwant\n  %+v\n%s", summary.Gaps.Excluded,
					testCase.excluded, output)
			}
		})
	}
}

// yieldEveryFile writes a yield file, as a parent run of several projects does, that leaves every .ts file of files
// to a nearer project, and returns its path. Each is named as the directory was given and with its links resolved,
// since the program names its files by the working directory, which the operating system may have resolved.
func yieldEveryFile(test *testing.T, root string, files map[string]string) string {
	test.Helper()
	resolvedRoot, evalSymlinksError := filepath.EvalSymlinks(root)
	if evalSymlinksError != nil {
		test.Fatal(evalSymlinksError)
	}
	yield := projectYield{Files: []string{}, Directories: []string{}}
	for name := range files {
		if strings.HasSuffix(name, ".ts") {
			yield.Files = append(yield.Files, filepath.Join(root, name), filepath.Join(resolvedRoot, name))
		}
	}
	encoded, marshalError := json.Marshal(yield)
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	directory := test.TempDir()
	writeTree(test, directory, map[string]string{"yield.json": string(encoded)})
	return filepath.Join(directory, "yield.json")
}
