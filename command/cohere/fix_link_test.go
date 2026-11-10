package main

import (
	"os"
	"path/filepath"
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
	for _, arguments := range [][]string{{"--fix"}, {}} {
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
		if !strings.Contains(output, ", 0 refused") {
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
		for _, reached := range []string{
			filepath.Join(root, "linked", "Outside.ts") + " → " +
				realPathIn(test, base, "outside/directory/Outside.ts"),
			filepath.Join(root, "LinkedFile.ts") + " → " + realPathIn(test, base, "outside/file/File.ts"),
		} {
			if !strings.Contains(output, "not written: "+reached) {
				test.Fatalf("%v: the run did not name %q as not written:\n%s", arguments, reached, output)
			}
		}
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
