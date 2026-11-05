package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/edit"
)

// TestTheFormatOnlyGateExitsOnFormattingAlone is the commit gate's command, through the binary: `--no-fix
// --format-only` exits 0 over a tree whose only findings are lint's, and nonzero, naming the file, when
// formatting would change one in the repository or in a nested repository it reads, or when the formatter
// could not read one there.
func TestTheFormatOnlyGateExitsOnFormattingAlone(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	debugged := "export function value(): number {\n  debugger;\n  return 1;\n}\n"
	writeTree(test, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      `{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}`,
		"NexusCohereSettings.json": `{"format":{"ignore":["tsconfig.json"]}}`,
		".gitignore":               ".cache/\n",
		// Formatted, and a lint finding with a fix: the gate's verdict must not hear it.
		"Producer.ts":         debugged,
		".gitmodules":         "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":        "gitdir: ../.git/modules/library\n",
		"library/Inner.ts":    "export const inner = 1;\n",
		"library/Debugged.ts": debugged,
	})
	// The settings files are formatted by a writing run first, which proposes no fixes either.
	if output, code := runCohere(test, binary, root, "--format-only"); code != 0 {
		test.Fatalf("the writing format-only run exited %d:\n%s", code, output)
	}
	if got := readForTest(test, filepath.Join(root, "Producer.ts")); got != debugged {
		test.Fatalf("--format-only applied a lint fix:\n%s", got)
	}
	// A file formatting does change is re-linted on its next pass in a run that fixes. Here nothing may
	// propose: it is formatted, and its finding is left for the run that fixes.
	writeTree(
		test,
		root,
		map[string]string{"Reformatted.ts": "export function other(): number {\n  debugger;\n  return   2;\n}\n"},
	)
	if output, code := runCohere(test, binary, root, "--format-only", "Reformatted.ts"); code != 0 {
		test.Fatalf("the writing format-only run exited %d:\n%s", code, output)
	}
	if got := readForTest(
		test,
		filepath.Join(root, "Reformatted.ts"),
	); got != "export function other(): number {\n  debugger;\n  return 2;\n}\n" {
		test.Fatalf("--format-only did not format the file, or fixed it once formatting changed it:\n%s", got)
	}

	gate := func(label string, wantExit int, wantLines ...string) {
		test.Helper()
		for _, arguments := range [][]string{
			{"--no-fix", "--format-only"},
			{"--no-fix", "--format-only", "--format-all"},
		} {
			output, code := runCohere(test, binary, root, arguments...)
			if code != wantExit {
				test.Fatalf("%s, %v: exit %d, want %d:\n%s", label, arguments, code, wantExit, output)
			}
			for _, line := range wantLines {
				if !strings.Contains(output, line) {
					test.Fatalf("%s, %v: missing %q:\n%s", label, arguments, line, output)
				}
			}
			if !strings.Contains(output, "fix checked in") ||
				!strings.Contains(output, "(formatting only, no fixes proposed)") ||
				!strings.Contains(output, "types skipped (not requested) · lint skipped (not requested)") {
				test.Fatalf("%s, %v: the phase line does not say what ran:\n%s", label, arguments, output)
			}
		}
	}

	// Nothing to format, lint findings present here and in the library: 0. A whole run says otherwise.
	gate("lint findings and nothing to format", 0, "0 files would change")
	if _, code := runCohere(test, binary, root, "--no-fix"); code == 0 {
		test.Fatalf("the lint finding was not a finding to a whole run, so this test proves nothing")
	}

	inner := filepath.Join(root, "library", "Inner.ts")
	writeTree(test, root, map[string]string{"library/Inner.ts": "export const inner   =   1;\n"})
	gate(
		"an unformatted file in a nested repository",
		1,
		inner+":1:1 - the formatter would rewrite this file in nested repository library",
	)
	writeTree(test, root, map[string]string{"library/Inner.ts": "export const inner = 1;\n"})

	ugly := filepath.Join(root, "Ugly.ts")
	writeTree(test, root, map[string]string{"Ugly.ts": "export const ugly   =   1;\n"})
	gate(
		"an unformatted file in the repository",
		1,
		ugly+":1:1 - --fix would rewrite this file: format [fix/would-change]",
	)
	removeForTest(test, ugly)

	broken := filepath.Join(root, "broken.json")
	writeTree(test, root, map[string]string{"broken.json": "{ \"a\": \n"})
	gate("a file the formatter cannot read in the repository", 1,
		broken+":1:1 - the formatter could not check this file: "+unparseableSkipReason+" [format/unchecked]")
	removeForTest(test, broken)

	nestedBroken := filepath.Join(root, "library", "broken.json")
	writeTree(test, root, map[string]string{"library/broken.json": "{ \"a\": \n"})
	gate("a file the formatter cannot read in a nested repository", 1,
		nestedBroken+
			":1:1 - the formatter could not check this file: in nested repository library, "+
			unparseableSkipReason+
			" [format/unchecked]")
	removeForTest(test, nestedBroken)

	gate("neither", 0, "0 files would change")

	for _, other := range []string{"--fix", "--types", "--lint", "--unused"} {
		output, code := runCohere(test, binary, root, "--no-fix", "--format-only", other)
		if code == 0 || !strings.Contains(output, "--format-only and "+other+" contradict each other") {
			test.Fatalf("--format-only with %s was not refused by name, exit %d:\n%s", other, code, output)
		}
	}
}

// TestOnlyAFileTheFormatterCouldNotReadIsUnchecked: a skip that is about the file (it does not parse) or a
// formatter that broke on it is a file nobody checked; a skip about the run (outside the named paths, a
// type the formatter does not handle) is a file that was never the run's to check.
func TestOnlyAFileTheFormatterCouldNotReadIsUnchecked(test *testing.T) {
	test.Parallel()
	summary := edit.Summary{
		NotTransformed: []edit.NotTransformedFile{
			{FileName: "/a/Broken.json", Reason: unparseableSkipReason},
			{FileName: "/a/Crashed.ts", Reason: "the whole-text transform failed (boom)", Failed: true},
			{FileName: "/a/Elsewhere.ts", Reason: "outside the format scope, 1 named path"},
			{FileName: "/a/notes.txt", Reason: ".txt is not a file type the formatter handles"},
		},
		FilesFailed:  []string{"/a/Unreadable.ts"},
		FilesRefused: []edit.RefusedFile{{FileName: "/a/Reprinted.ts", Rules: []string{"format"}}},
	}
	got := []string{}
	for _, file := range formatOnlyUnchecked(summary, formatScope{}) {
		got = append(got, filepath.Base(file.FileName))
	}
	if strings.Join(got, ", ") != "Broken.json, Crashed.ts, Unreadable.ts, Reprinted.ts" {
		test.Fatalf("unchecked %v", got)
	}

	failed := formatOnlyUnchecked(edit.Summary{}, formatScope{failure: os.ErrPermission})
	if len(failed) != 1 || failed[0].FileName != "" || !strings.Contains(failed[0].Reason, "the format walk failed") {
		test.Fatalf("a failed walk was not a finding: %+v", failed)
	}
}

func removeForTest(test *testing.T, fileName string) {
	test.Helper()
	if removeError := os.Remove(fileName); removeError != nil {
		test.Fatal(removeError)
	}
}

// TestFormatOnlyBuildsNoGraph: `--format-only` reads the disk, never the program (#m0dktbn). Its tsconfig
// here cannot build a program at all (kept out of the format scope, so only a build would read it), so a
// whole run fails building the graph, and a format-only run must not notice: it checks, formats and says
// it built no graph.
func TestFormatOnlyBuildsNoGraph(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":            "{ \"compilerOptions\": \n",
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\" }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [\"tsconfig.json\"] } }\n",
		".gitignore":               ".cache/\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
	})
	if output, code := runCohere(
		test,
		binary,
		root,
		"--no-fix",
	); code == 0 || !strings.Contains(output, "Building the type graph") {
		test.Fatalf("the tsconfig built a program, so this test proves nothing, exit %d:\n%s", code, output)
	}

	output, code := runCohere(test, binary, root, "--no-fix", "--format-only")
	if code != 0 ||
		!strings.Contains(output, "graph not built: --format-only reads the disk, not the program") ||
		strings.Contains(output, "graph built in") ||
		!strings.Contains(output, "no graph built (formatting reads none)") {
		test.Fatalf("--format-only built the graph or did not say it did not, exit %d:\n%s", code, output)
	}

	writeTree(test, root, map[string]string{"Ugly.ts": "export const ugly   =   1;\n"})
	output, code = runCohere(test, binary, root, "--no-fix", "--format-only")
	if code == 0 ||
		!strings.Contains(output, filepath.Join(root, "Ugly.ts")+":1:1 - --fix would rewrite this file: format") {
		test.Fatalf("with no graph, an unformatted file was not reported, exit %d:\n%s", code, output)
	}
	if output, code = runCohere(
		test,
		binary,
		root,
		"--format-only",
		"Ugly.ts",
	); code != 0 || readForTest(test, filepath.Join(root, "Ugly.ts")) != "export const ugly = 1;\n" {
		test.Fatalf("with no graph, the writing run did not format the named file, exit %d:\n%s", code, output)
	}
}
