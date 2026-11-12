//go:build unix

// The stand-in Swift engine here is a /bin/sh script, so the file builds on Unix.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// A real run at a root holding a TypeScript program and a Swift package ends its default view on one footer line,
// the two projects' rules summed, and no line of its own per project; `--verbose` keeps each project's (#d2xga9h).
// The one line exists only when each project's run hands its summary back, so this goes red if a child prints its
// own footer instead or the root stops reading what it was handed.
func TestAMixedRootEndsOnOneFooterLine(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture, absoluteError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract", "Clean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	engine := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(
		engine,
		[]byte("#!/bin/sh\ncat '"+fixture+"'\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".gitignore": ".cache/\n",
		// Formatted, so the root's format project has nothing to say and the line is green.
		"tsconfig.json":       "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, \"include\": [\"source\"] }\n",
		"source/Tidy.ts":      "export const tidy = 1;\n",
		"swift/Package.swift": "// swift-tools-version: 6.0\n",
	})
	run := func(arguments ...string) []string {
		command := exec.Command(binary, append([]string{"--no-fix"}, arguments...)...)
		command.Dir = root
		command.Env = append(os.Environ(), "COHERE_SWIFT_ENGINE="+engine)
		// A failing run is read from its lines like a passing one: which footers it printed is the question.
		output, runError := command.CombinedOutput()
		if runError != nil {
			test.Logf("cohere %s ended: %v", strings.Join(arguments, " "), runError)
		}
		footers := []string{}
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, "✓ 💎") || strings.Contains(line, "✗ ☠️") {
				footers = append(footers, line)
			}
		}
		return footers
	}
	footers := run()
	if len(footers) != 1 || !strings.HasPrefix(footers[0], "✓ 💎 ") || strings.Contains(footers[0], "projects") {
		test.Fatalf("the default view printed %d footer lines, want one combined line:\n%s", len(footers),
			strings.Join(footers, "\n"))
	}
	verbose := run("--verbose")
	if len(verbose) != 3 || !strings.Contains(verbose[2], "2 projects") {
		test.Errorf("--verbose printed %d footer lines, want each project's and the line counting them:\n%s",
			len(verbose), strings.Join(verbose, "\n"))
	}
}
