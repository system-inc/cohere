//go:build unix

// The stand-in Swift engine here is a /bin/sh script, so the file builds on Unix.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// TestAMixedRunUnderJsonWritesOnlyRecordsToStdout: a repository holding a TypeScript program and a Swift
// package is checked as two projects, and under `--json` every line of stdout is a record, while what a
// project's run wrote to stderr, here the note that the Swift engine was named by the override, reaches
// stderr. The two used to share stdout, so a program reading the records met a line of prose.
func TestAMixedRunUnderJsonWritesOnlyRecordsToStdout(test *testing.T) {
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
		".gitignore":          ".cache/\n",
		"tsconfig.json":       `{"compilerOptions":{"strict":true,"noEmit":true},"include":["source"]}`,
		"source/Tidy.ts":      "export const tidy = 1;\n",
		"swift/Package.swift": "// swift-tools-version: 6.0\n",
	})

	command := exec.Command(binary, "--no-fix", "--json")
	command.Dir = root
	// Never a replay: the run record lives in the root's own .cache/cohere, and the root is new.
	command.Env = append(os.Environ(), "COHERE_SWIFT_ENGINE="+engine)
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	command.Stdout, command.Stderr = &standardOutput, &standardError
	runError := command.Run()
	var exitError *exec.ExitError
	if runError != nil && !errors.As(runError, &exitError) {
		test.Fatalf("cohere did not run: %v", runError)
	}

	labels := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(standardOutput.String()), "\n") {
		var record struct {
			Kind  string `json:"kind"`
			Label string `json:"label"`
		}
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("stdout under --json holds a line that is not a record: %q\nstdout:\n%s",
				line, standardOutput.String())
		}
		if record.Kind == "summary" {
			labels[record.Label] = true
		}
	}
	if len(labels) != 2 {
		test.Errorf("expected a summary record for each of the two projects, found %v:\n%s",
			labels, standardOutput.String())
	}
	if !strings.Contains(standardError.String(), "named by COHERE_SWIFT_ENGINE") {
		test.Errorf("the Swift project's note did not reach stderr:\nstderr:\n%s", standardError.String())
	}
}
