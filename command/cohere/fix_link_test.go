package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/write_root"
)

// writeRootOf is root's WriteRoot, for a test that runs the fix phase over a fixture under root.
func writeRootOf(test testing.TB, root string) write_root.WriteRoot {
	test.Helper()
	writeRoot, rootError := write_root.New(root)
	if rootError != nil {
		test.Fatalf("resolving the root %s: %v", root, rootError)
	}
	return writeRoot
}

// The shapes codebase's probe on #hwz114g found, run through the binary as a person would (#td3zta2): a root
// with a directory linked to one outside it, a file linked to a file outside it, a file linked to one inside
// it, and a plain file, each holding a `let` prefer-const rewrites. `--fix` and a bare run fix and format the
// plain file and write the inside link's target through it; they leave both outside files byte for byte as
// they were, keep every link a link, and name each file not written with where it really is. Before the
// fix, the run rewrote both outside files' text or replaced the file link with a plain file, exited 0 and
// named nothing.
func TestAFixRunNeverWritesThroughALinkOutsideTheRoot(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, arguments := range [][]string{{"--fix"}, {}, {"--fix", "--json"}} {
		asJson := len(arguments) == 2
		base := test.TempDir()
		root := filepath.Join(base, "root")
		source := "let value = 1;\nexport { value };\n"
		writeTree(test, base, map[string]string{
			"root/tsconfig.json":           `{"compilerOptions":{"strict":true,"noEmit":true},"include":["**/*.ts"]}`,
			"root/CohereSettings.json":     `{"rules":{"prefer-const":"error"}}`,
			"root/Plain.ts":                source,
			"root/real/Inside.ts":          source,
			"outside/directory/Outside.ts": source,
			"outside/file/File.ts":         source,
		})
		for name, target := range map[string]string{
			"linked":        filepath.Join("..", "outside", "directory"),
			"LinkedFile.ts": filepath.Join("..", "outside", "file", "File.ts"),
			"LinkedIn.ts":   filepath.Join("real", "Inside.ts"),
		} {
			if linkError := os.Symlink(target, filepath.Join(root, name)); linkError != nil {
				test.Fatal(linkError)
			}
		}

		output, _ := runCohere(test, binary, root, arguments...)
		// LinkedIn.ts and real/Inside.ts are one file: fixed under both names, the second applied proposals
		// measured against the text the first replaced, and the parse guard refused it.
		if !asJson && !strings.Contains(output, ", 0 refused") {
			test.Fatalf("%v: a fix was refused, so one file was fixed under two names:\n%s", arguments, output)
		}

		for _, outside := range []string{"outside/directory/Outside.ts", "outside/file/File.ts"} {
			contents, readError := os.ReadFile(filepath.Join(base, outside))
			if readError != nil {
				test.Fatal(readError)
			}
			if string(contents) != source {
				test.Fatalf("%v: %s, outside the root, was rewritten:\n%s\n%s", arguments, outside, contents, output)
			}
		}
		for _, linked := range []string{"linked", "LinkedFile.ts", "LinkedIn.ts"} {
			information, statError := os.Lstat(filepath.Join(root, linked))
			if statError != nil {
				test.Fatal(statError)
			}
			if information.Mode()&os.ModeSymlink == 0 {
				test.Fatalf("%v: %s was a link and is now a plain file\n%s", arguments, linked, output)
			}
		}
		for _, written := range []string{"root/Plain.ts", "root/real/Inside.ts"} {
			contents, readError := os.ReadFile(filepath.Join(base, written))
			if readError != nil {
				test.Fatal(readError)
			}
			if !strings.Contains(string(contents), "const value") {
				test.Fatalf("%v: %s, inside the root, was not fixed, so the run proves nothing:\n%s\n%s", arguments,
					written, contents, output)
			}
		}
		notWritten := []notWrittenFile{
			{File: filepath.Join(root, "LinkedFile.ts"), RealPath: realPathIn(test, base, "outside/file/File.ts")},
			{
				File:     filepath.Join(root, "linked", "Outside.ts"),
				RealPath: realPathIn(test, base, "outside/directory/Outside.ts"),
			},
		}
		if asJson {
			assertGapNamesNotWritten(test, output, notWritten)
			continue
		}
		// Each by name with where it really is, the footer's gap, and the run read as short, in the Go engine's
		// words (#hwz114g), so a run that withheld a write never ends on a clean bill of health.
		for _, file := range notWritten {
			if reached := file.File + " → " + file.RealPath; !strings.Contains(output, "not written: "+reached) {
				test.Fatalf("%v: the run did not name %q as not written:\n%s", arguments, reached, output)
			}
		}
		said := []string{"⚠ not written: 2 files reached through a link outside the root"}
		// `--fix` alone skips the type check and the lint, so its line says the phases fell short; a bare run checks
		// every phase, and the files not written are what it says.
		if len(arguments) == 0 {
			said = append(said, "this run did not check everything: "+notWrittenSentence)
		}
		for _, said := range said {
			if !strings.Contains(output, said) {
				test.Fatalf("%v: the run did not say %q:\n%s", arguments, said, output)
			}
		}
	}
}

// assertGapNamesNotWritten fails unless output's summary record holds want as its gaps' notWritten, in order.
func assertGapNamesNotWritten(test *testing.T, output string, want []notWrittenFile) {
	test.Helper()
	for line := range strings.Lines(output) {
		var record struct {
			Kind string  `json:"kind"`
			Gaps runGaps `json:"gaps"`
		}
		if json.Unmarshal([]byte(line), &record) != nil || record.Kind != "summary" {
			continue
		}
		if !reflect.DeepEqual(record.Gaps.NotWritten, want) {
			test.Fatalf("--json's gap names %+v as not written, want %+v:\n%s", record.Gaps.NotWritten, want, output)
		}
		return
	}
	test.Fatalf("--json wrote no summary record:\n%s", output)
}

// oneNamePerRealFile keeps the name that is not itself a link, the real file a person opened, wherever it sorts:
// `LinkedIn.ts` sorts before `real/Inside.ts`, and `zlink.ts` after `real/Other.ts`, and each pair keeps the real
// file. A name of its own is kept, and the order of what is kept is the order given. Fails if the tie-break is
// dropped (the first in order wins) or turned around (the link wins).
func TestOneNamePerRealFileKeepsTheNameThatIsNotALink(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"real/Inside.ts": "export const inside = 1;\n",
		"real/Other.ts":  "export const other = 1;\n",
		"Plain.ts":       "export const plain = 1;\n",
	})
	for name, target := range map[string]string{
		"LinkedIn.ts": filepath.Join("real", "Inside.ts"),
		"zlink.ts":    filepath.Join("real", "Other.ts"),
	} {
		if linkError := os.Symlink(target, filepath.Join(root, name)); linkError != nil {
			test.Fatal(linkError)
		}
	}
	given := []string{
		filepath.Join(root, "LinkedIn.ts"),
		filepath.Join(root, "Plain.ts"),
		filepath.Join(root, "real", "Inside.ts"),
		filepath.Join(root, "real", "Other.ts"),
		filepath.Join(root, "zlink.ts"),
	}
	kept := oneNamePerRealFile(given, writeRootOf(test, root))
	want := []string{
		filepath.Join(root, "real", "Inside.ts"),
		filepath.Join(root, "Plain.ts"),
		filepath.Join(root, "real", "Other.ts"),
	}
	if strings.Join(kept, "\n") != strings.Join(want, "\n") {
		test.Fatalf("kept:\n%s\nwant:\n%s", strings.Join(kept, "\n"), strings.Join(want, "\n"))
	}
}

// realPathIn is name's real path under base, every link resolved, as the run names it.
func realPathIn(test *testing.T, base string, name string) string {
	test.Helper()
	resolved, resolveError := filepath.EvalSymlinks(filepath.Join(base, name))
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	return resolved
}
