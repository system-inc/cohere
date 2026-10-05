package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mixedRepository writes a repository holding a TypeScript program at its root and a Swift package below
// it, the shape a run from the root checks both of (#f9nftxz).
func mixedRepository(t *testing.T, swiftDirectory string) string {
	t.Helper()
	root := newRepository(t, map[string]string{
		"tsconfig.json":       incrementalFixtureConfig,
		"CohereSettings.json": `{"rules":{}}`,
		"index.ts":            "export const value = 1;\n",
		".gitignore":          "tsconfig.tsbuildinfo\n",
	})
	writeTree(t, filepath.Join(root, swiftDirectory), map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	return root
}

// runWithEngine runs cohere from a directory with the Swift engine the override names.
func runWithEngine(t *testing.T, binary string, engine string, directory string, arguments ...string) (string, int) {
	t.Helper()
	command := exec.Command(binary, verboseArguments(arguments)...)
	command.Dir = directory
	command.Env = append(os.Environ(), "COHERE_SWIFT_ENGINE="+engine, "COHERE_VERDICT_FD=")
	output, err := command.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	exited, isExit := err.(*exec.ExitError)
	if !isExit {
		t.Fatalf("running cohere: %v\n%s", err, output)
	}
	return string(output), exited.ExitCode()
}

// A mixed repository is checked whole from its root: each project in its own section under its own
// engine, one line per project, and one summary. The run is green only when both are, a Swift finding
// turns it red, and its exit code is the one the failing project's own run gives.
func TestAMixedRepositoryIsCheckedWholeAndTheWorstExitWins(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	clean := fakeSwiftEngine(t, "cat "+filepath.Join(contractDirectory(t), "Clean.jsonl"))
	findings := fakeSwiftEngine(t, "cat "+filepath.Join(contractDirectory(t), "Findings.jsonl")+"\nexit 1")

	root := mixedRepository(t, "apple/Toy")
	output, exitCode := runWithEngine(t, binary, clean, root, "--no-fix", "--no-format")
	if exitCode != 0 {
		t.Fatalf("a clean mixed repository exited %d:\n%s", exitCode, output)
	}
	for _, want := range []string{
		"== . (TypeScript) ==",
		"== apple/Toy (Swift) ==",
		"  . (TypeScript): green",
		"  apple/Toy (Swift): green",
		"projects: 2 checked (1 TypeScript, 1 Swift), every one green",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("the report lacks %q:\n%s", want, output)
		}
	}

	output, exitCode = runWithEngine(t, binary, findings, root, "--no-fix", "--no-format")
	_, alone := runWithEngine(t, binary, findings, root, "--no-fix", "--no-format", "--directory", filepath.Join(root, "apple", "Toy"))
	if exitCode == 0 || exitCode != alone {
		t.Fatalf("a Swift finding exited %d from the root and %d from the package alone, want the same nonzero code:\n%s", exitCode, alone, output)
	}
	if !strings.Contains(output, "apple/Toy (Swift): failed, exit") || !strings.Contains(output, "1 failed") {
		t.Errorf("the report does not name the failing project:\n%s", output)
	}
	if !strings.Contains(output, "  . (TypeScript): green") {
		t.Errorf("the TypeScript project's own verdict is lost:\n%s", output)
	}
}

// Both markers in one directory are two projects, each checked by its own engine; and projects nested two
// levels down are found from the root.
func TestBothMarkersInOneDirectoryAndProjectsNestedDeepAreEachChecked(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	clean := fakeSwiftEngine(t, "cat "+filepath.Join(contractDirectory(t), "Clean.jsonl"))

	together := mixedRepository(t, ".")
	output, exitCode := runWithEngine(t, binary, clean, together, "--no-fix", "--no-format")
	if exitCode != 0 || !strings.Contains(output, "== . (TypeScript) ==") || !strings.Contains(output, "== . (Swift) ==") {
		t.Fatalf("a directory holding both markers was not checked by both engines (exit %d):\n%s", exitCode, output)
	}

	deep := newRepository(t, nil)
	writeTree(t, filepath.Join(deep, "web", "app"), map[string]string{
		"tsconfig.json":       incrementalFixtureConfig,
		"CohereSettings.json": `{"rules":{}}`,
		"index.ts":            "export const value = 1;\n",
	})
	writeTree(t, filepath.Join(deep, "apple", "Toy"), map[string]string{"Package.swift": discoveryPackage, "Sources/Toy/Toy.swift": "let toy = 1\n"})
	output, exitCode = runWithEngine(t, binary, clean, deep, "--no-fix", "--no-format")
	if exitCode != 0 || !strings.Contains(output, "== apple/Toy (Swift) ==") || !strings.Contains(output, "== web/app (TypeScript) ==") {
		t.Fatalf("projects two levels down were not both checked (exit %d):\n%s", exitCode, output)
	}
}

// A project whose run fails without a verdict, here an engine that dies before reporting, fails the run
// from the root; and a root holding no project at all fails loudly rather than passing over nothing.
func TestACrashedProjectOrNoProjectFailsTheRun(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	crashed := fakeSwiftEngine(t, "exit 2")
	root := mixedRepository(t, "apple/Toy")
	output, exitCode := runWithEngine(t, binary, crashed, root, "--no-fix", "--no-format")
	if exitCode == 0 || !strings.Contains(output, "apple/Toy (Swift): failed") {
		t.Fatalf("a Swift engine that died passed the run (exit %d):\n%s", exitCode, output)
	}

	empty := newRepository(t, map[string]string{"README.md": "nothing to check\n"})
	output, exitCode = runWithEngine(t, binary, crashed, empty)
	if exitCode == 0 || !strings.Contains(output, "no project here to check") {
		t.Fatalf("a repository with no project passed (exit %d):\n%s", exitCode, output)
	}
}

// contractDirectory is where the Swift contract's fixtures are.
func contractDirectory(t *testing.T) string {
	t.Helper()
	directory, err := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if err != nil {
		t.Fatal(err)
	}
	return directory
}
