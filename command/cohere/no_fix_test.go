package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/format/format_files"
)

// treeSnapshot reads every file under a root, so a run can be held to having written nothing at all:
// no changed byte, and no new file either.
func treeSnapshot(test *testing.T, root string) map[string]string {
	test.Helper()
	snapshot := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		// cohere's own cache for the project is outside `--no-fix`'s promise, which is about source: it is
		// cohere's to keep, and `--no-cache` is the flag that writes none of it. So is a nested
		// repository's, where the drift check keeps that repository's format record.
		if pathError == nil && entry.IsDir() && filepath.Base(filepath.Dir(path)) == ".cache" {
			if owner := filepath.Dir(
				filepath.Dir(path),
			); path == cache_directory.Of(owner) && (owner == root || format_files.HasOwnRepository(owner)) {
				return filepath.SkipDir
			}
		}
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		snapshot[path] = string(contents)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return snapshot
}

func assertTreeUnchanged(test *testing.T, root string, before map[string]string, output string) {
	test.Helper()
	after := treeSnapshot(test, root)
	for path, contents := range before {
		if after[path] != contents {
			test.Errorf(
				"--no-fix changed %s:\nbefore:\n%s\nafter:\n%s\noutput:\n%s",
				path,
				contents,
				after[path],
				output,
			)
		}
	}
	for path := range after {
		if _, existed := before[path]; !existed {
			test.Errorf("--no-fix created %s\noutput:\n%s", path, output)
		}
	}
}

// TestNoFixReportsWhatFixAndFormatWouldChange is `--no-fix`'s promise, both halves: it reports every
// file `--fix` would rewrite, naming the formatter or the fixing rule, and it writes nothing.
//
// It used to skip the fix phase, so an unformatted file passed a clean `--no-fix` run with
// `lint: 0 findings`. Each step's control is the writing run: a file the check reports is one `--fix`
// then rewrites, and after that rewrite the check is silent, so the two cannot disagree in either
// direction.
func TestNoFixReportsWhatFixAndFormatWouldChange(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      `{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}`,
		"NexusCohereSettings.json": `{"format":{"ignore":[]}}`,
		"Clean.ts":                 "export const clean = 1;\n",
	})

	// Whatever the formatter wants of the fixture's own files, a writing run settles it. `--format-all`
	// alone, without `--format`, because naming the scope asks for formatting.
	if output, code := runCohere(test, binary, root, "--fix", "--format-all"); code != 0 {
		test.Fatalf("settling the fixture exited %d:\n%s", code, output)
	}

	// A formatted tree is silent, and the phase line says it looked.
	before := treeSnapshot(test, root)
	output, code := runCohere(test, binary, root, "--no-fix", "--format-all")
	if code != 0 {
		test.Fatalf("a formatted tree failed --no-fix with exit %d:\n%s", code, output)
	}
	if !strings.Contains(output, "fix checked in") || !strings.Contains(output, "0 files would change") {
		test.Errorf("the phase line does not say the fix phase was checked:\n%s", output)
	}
	if strings.Contains(output, "would-change") {
		test.Errorf("a formatted tree reported a file that would change:\n%s", output)
	}
	assertTreeUnchanged(test, root, before, output)

	// An unformatted file is a finding against that file, naming the formatter, and stays as it was.
	unformatted := "export   const  spaced = {a:1}\n"
	writeTree(test, root, map[string]string{"Unformatted.ts": unformatted})
	before = treeSnapshot(test, root)
	output, code = runCohere(test, binary, root, "--no-fix", "--format-all")
	if code == 0 {
		test.Fatalf("an unformatted file passed --no-fix:\n%s", output)
	}
	want := filepath.Join(root, "Unformatted.ts") + ":1:1 - --fix would rewrite this file: format [fix/would-change]"
	if !strings.Contains(output, want) {
		test.Errorf("want %q in:\n%s", want, output)
	}
	if !strings.Contains(output, "1 file would change") || !strings.Contains(output, "1 would be reformatted") {
		test.Errorf("the count did not reach the phase line and the summary:\n%s", output)
	}
	assertTreeUnchanged(test, root, before, output)

	// The control: the writing run rewrites exactly the file the check named, and the check is then
	// silent again.
	if output, code := runCohere(test, binary, root, "--fix", "--format-all"); code != 0 {
		test.Fatalf("the writing run exited %d:\n%s", code, output)
	}
	if readForTest(test, filepath.Join(root, "Unformatted.ts")) == unformatted {
		test.Fatal("--fix left the file --no-fix reported, so the two disagree")
	}
	if output, code := runCohere(test, binary, root, "--no-fix", "--format-all"); code != 0 {
		test.Fatalf("--no-fix still failed after --fix wrote what it reported, exit %d:\n%s", code, output)
	}

	// A fixable finding with formatting left out names its rule alone. A bare --no-fix formats too (#b1sjy7b),
	// so it names the formatter beside the rule, since the text the fix leaves is reformatted.
	writeTree(test, root, map[string]string{"Debugger.ts": "export function stop(): void {\n    debugger;\n}\n"})
	before = treeSnapshot(test, root)
	for _, testCase := range []struct {
		arguments []string
		changers  string
	}{
		{[]string{"--no-fix", "--no-format"}, "no-debugger"},
		{[]string{"--no-fix"}, "no-debugger, format"},
	} {
		output, code = runCohere(test, binary, root, testCase.arguments...)
		if code == 0 {
			test.Fatalf("a fixable finding passed %v:\n%s", testCase.arguments, output)
		}
		want = filepath.Join(root, "Debugger.ts") +
			":1:1 - --fix would rewrite this file: " +
			testCase.changers +
			" [fix/would-change]"
		if !strings.Contains(output, want) {
			test.Errorf("%v: want %q in:\n%s", testCase.arguments, want, output)
		}
		assertTreeUnchanged(test, root, before, output)
	}
}
