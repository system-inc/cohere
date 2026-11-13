package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// TestADefaultRunPrintsNoNotes is the default view end to end, golden: a clean run whose cache table was
// just thrown away prints its footer and nothing else. Notes are --verbose's, so the line that says the
// table was discarded, and the one that says .gitignore does not ignore the cache, print only there.
//
// This is the run ahra saw after a cache format bump: two `note: cache table discarded` lines above the
// footer. A table from another format and a table that is not one take the same path and print the same
// note, so the table here is overwritten rather than written by an older build. Both halves are held: the
// default run read the table and rewrote it, so the note had something to say, and --verbose says it.
func TestADefaultRunPrintsNoNotes(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true,"target":"es2022","module":"esnext","moduleResolution":"bundler"},"include":["source"]}`,
		"CohereSettings.json": `{"rules":{}}`,
		"source/index.ts":     "export const value = 1;\n",
		// Left without `.cache/` on purpose: that note is one of the two a default run must not print.
		".gitignore": "node_modules/\n",
	})
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{
			"-c",
			"user.email=fixture@example.com",
			"-c",
			"user.name=fixture",
			"commit",
			"--quiet",
			"--allow-empty",
			"-m",
			"initial",
		},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = root
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, outputError, output)
		}
	}

	// The fixture's JSON is not in the house format, and a run that rewrites it lists the files it cohered
	// above the footer. Formatting is not this test's subject.
	run := func(arguments ...string) (string, int) {
		test.Helper()
		command := exec.Command(binary, append([]string{"--no-format"}, arguments...)...)
		command.Dir = root
		output, outputError := command.CombinedOutput()
		if outputError == nil {
			return string(output), 0
		}
		exitError, isExit := outputError.(*exec.ExitError)
		if !isExit {
			test.Fatalf("running cohere: %v\n%s", outputError, output)
		}
		return string(output), childExitCode(test, exitError)
	}
	discardTable := func() {
		test.Helper()
		tables := cacheTableFiles(test, root)
		if len(tables) == 0 {
			test.Fatalf("no cache table in %s, so there is nothing for a run to discard", root)
		}
		for _, table := range tables {
			if writeError := os.WriteFile(table, []byte("not a cache table"), 0o600); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}

	if output, code := run(); code != 0 {
		test.Fatalf("exit %d from the first run of a clean project:\n%s", code, output)
	}
	// The first run made the cache directory ignoring itself (#6vhp0vd). Without its ignore file it is the cache an
	// older cohere made, which the tracked-cache note is still said of, so the default run has that note to hide.
	if removeError := os.Remove(filepath.Join(cache_directory.Of(root), ".gitignore")); removeError != nil {
		test.Fatal(removeError)
	}
	discardTable()

	output, code := run()
	if code != 0 {
		test.Fatalf("exit %d from the run after the discard:\n%s", code, output)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "✓ 💎 ") {
		test.Errorf("the default run after a discard printed more than its footer:\n%s", output)
	}
	if strings.Contains(output, "note:") {
		test.Errorf("the default run printed a note:\n%s", output)
	}
	for _, table := range cacheTableFiles(test, root) {
		if contents, readError := os.ReadFile(table); readError == nil && string(contents) == "not a cache table" {
			test.Fatalf("the run never read the discarded table, so its silence proves nothing: %s", table)
		}
	}

	// The control: the same discard under --verbose says both notes, so the default run had them to hide.
	discardTable()
	verbose, _ := run("--verbose")
	for _, want := range []string{"note: cache table discarded", "does not ignore .cache/"} {
		if !strings.Contains(verbose, want) {
			test.Errorf("--verbose did not say %q after a discard:\n%s", want, verbose)
		}
	}
}
