//go:build unix

// Every project here is checked through a stand-in Swift engine, a /bin/sh script, so the file builds on
// Unix, as swift_engine_test.go does (#tejf9bc).

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mixedRepository writes a repository holding a TypeScript program at its root and a Swift package below
// it, the shape a run from the root checks both of (#f9nftxz).
//
// Its files are in the house format, since a run formats them and the Swift engine refuses --no-format
// (#nqb3mjv), so these runs check formatting too.
func mixedRepository(test *testing.T, swiftDirectory string) string {
	test.Helper()
	root := newRepository(test, map[string]string{
		"tsconfig.json":       incrementalFixtureConfig + "\n",
		"CohereSettings.json": "{ \"rules\": {} }\n",
		"index.ts":            "export const value = 1;\n",
		".gitignore":          "tsconfig.tsbuildinfo\n",
	})
	writeTree(test, filepath.Join(root, swiftDirectory), map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
	})
	return root
}

// A mixed repository is checked whole from its root: each project in its own section under its own
// engine, one line per project, and one summary. The run is green only when both are, a Swift finding
// turns it red, and its exit code is the one the failing project's own run gives.
func TestAMixedRepositoryIsCheckedWholeAndTheWorstExitWins(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	clean := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Clean.jsonl"))
	findings := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Findings.jsonl")+"\nexit 1")

	root := mixedRepository(test, "apple/Toy")
	output, exitCode := runWithEngine(test, binary, clean, root, "--no-fix")
	if exitCode != 0 {
		test.Fatalf("a clean mixed repository exited %d:\n%s", exitCode, output)
	}
	for _, want := range []string{
		"== . (TypeScript) ==",
		"== apple/Toy (Swift) ==",
		"  . (TypeScript): green",
		"  apple/Toy (Swift): green",
		"projects: 2 checked (1 TypeScript, 1 Swift), every one green",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the report lacks %q:\n%s", want, output)
		}
	}

	output, exitCode = runWithEngine(test, binary, findings, root, "--no-fix")
	_, alone := runWithEngine(test, binary, findings, root, "--no-fix", "--directory", filepath.Join(root, "apple", "Toy"))
	if exitCode == 0 || exitCode != alone {
		test.Fatalf("a Swift finding exited %d from the root and %d from the package alone, want the same nonzero code:\n%s", exitCode, alone, output)
	}
	if !strings.Contains(output, "apple/Toy (Swift): failed, exit") || !strings.Contains(output, "1 failed") {
		test.Errorf("the report does not name the failing project:\n%s", output)
	}
	if !strings.Contains(output, "  . (TypeScript): green") {
		test.Errorf("the TypeScript project's own verdict is lost:\n%s", output)
	}
}

// Both markers in one directory are two projects, each checked by its own engine; and projects nested two
// levels down are found from the root.
func TestBothMarkersInOneDirectoryAndProjectsNestedDeepAreEachChecked(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	clean := fakeSwiftEngine(test, "cat "+filepath.Join(contractDirectory(test), "Clean.jsonl"))

	together := mixedRepository(test, ".")
	output, exitCode := runWithEngine(test, binary, clean, together, "--no-fix")
	if exitCode != 0 || !strings.Contains(output, "== . (TypeScript) ==") || !strings.Contains(output, "== . (Swift) ==") {
		test.Fatalf("a directory holding both markers was not checked by both engines (exit %d):\n%s", exitCode, output)
	}

	deep := newRepository(test, nil)
	writeTree(test, filepath.Join(deep, "web", "app"), map[string]string{
		"tsconfig.json":       incrementalFixtureConfig + "\n",
		"CohereSettings.json": "{ \"rules\": {} }\n",
		"index.ts":            "export const value = 1;\n",
	})
	writeTree(test, filepath.Join(deep, "apple", "Toy"), map[string]string{"Package.swift": discoveryPackage, "Sources/Toy/Toy.swift": "let toy = 1\n"})
	output, exitCode = runWithEngine(test, binary, clean, deep, "--no-fix")
	if exitCode != 0 || !strings.Contains(output, "== apple/Toy (Swift) ==") || !strings.Contains(output, "== web/app (TypeScript) ==") {
		test.Fatalf("projects two levels down were not both checked (exit %d):\n%s", exitCode, output)
	}
}

// A project whose run fails without a verdict, here an engine that dies before reporting, fails the run
// from the root; and a root holding no project at all fails loudly rather than passing over nothing.
func TestACrashedProjectOrNoProjectFailsTheRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	crashed := fakeSwiftEngine(test, "exit 2")
	root := mixedRepository(test, "apple/Toy")
	output, exitCode := runWithEngine(test, binary, crashed, root, "--no-fix")
	if exitCode == 0 || !strings.Contains(output, "apple/Toy (Swift): failed") {
		test.Fatalf("a Swift engine that died passed the run (exit %d):\n%s", exitCode, output)
	}

	empty := newRepository(test, map[string]string{"README.md": "nothing to check\n"})
	output, exitCode = runWithEngine(test, binary, crashed, empty)
	if exitCode == 0 || !strings.Contains(output, "no project here to check") {
		test.Fatalf("a repository with no project passed (exit %d):\n%s", exitCode, output)
	}
}

// Under --format-only a Swift package runs in the engine's own format-only mode (#nqb3mjv, c024df4b): the
// engine gets --format-only, its types and lint skipped as not requested are no gap, and its format finding
// fails the run, beside a TypeScript program and in the package alone. Before, the engine had no such mode,
// so the package was skipped with its gap named, and the flag against it alone was refused.
func TestFormatOnlyRunsTheSwiftEngineInItsFormatOnlyMode(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	arguments := filepath.Join(test.TempDir(), "arguments")
	engine := fakeSwiftEngine(test, "echo \"$@\" >> '"+arguments+"'\ncat "+filepath.Join(contractDirectory(test), "FormatOnly.jsonl")+"\nexit 1")
	root := mixedRepository(test, "apple/Toy")

	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix", "--format-only")
	if exitCode == 0 || !strings.Contains(output, "apple/Toy (Swift): failed, exit") || !strings.Contains(output, "  . (TypeScript): green") ||
		!strings.Contains(output, "projects: 2 checked (1 TypeScript, 1 Swift), 1 failed") {
		test.Errorf("beside a TypeScript program, the Swift format finding did not fail the run (exit %d):\n%s", exitCode, output)
	}
	if strings.Contains(output, "not requested") && strings.Contains(output, "did not run") {
		test.Errorf("the run counted types or lint skipped as not requested as a gap:\n%s", output)
	}

	output, alone := runWithEngine(test, binary, engine, root, "--no-fix", "--format-only", "--directory", filepath.Join(root, "apple", "Toy"))
	if alone == 0 || alone != exitCode || !strings.Contains(output, "notFormatted") && !strings.Contains(output, "Doubler.swift") {
		test.Errorf("the package alone did not fail on its format finding as the mixed run did (exit %d, mixed %d):\n%s", alone, exitCode, output)
	}
	logged, readError := os.ReadFile(arguments)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
		if !strings.Contains(" "+line+" ", " --no-fix ") || !strings.Contains(" "+line+" ", " --format-only ") {
			test.Errorf("the engine was run without --no-fix --format-only: %q", line)
		}
	}
	if count := len(strings.Split(strings.TrimSpace(string(logged)), "\n")); count != 2 {
		test.Errorf("the engine ran %d times, want twice:\n%s", count, logged)
	}
}

// contractDirectory is where the Swift contract's fixtures are.
func contractDirectory(test *testing.T) string {
	test.Helper()
	directory, absolutePathError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract"))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	return directory
}
