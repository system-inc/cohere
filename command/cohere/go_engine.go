package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// goEngineVerb is the hidden first argument that runs this binary as the Go engine rather than as the front
// door. Hidden because nobody types it: the front door runs it as a child, the way it runs cohere-swift.
const goEngineVerb = "go-engine"

// goFlags are the flags a Go run takes, as agreed with the engine on #a0cgmh6. `lint-config` is forwarded
// with its absolute path, and the engine reads that file's `go` block in place of the root's settings. Any
// flag in neither list is refused by name, among them the unused report, timing, explain, the tsconfig, the
// cache and fix passes, none of which the Go engine implements yet.
var goFlags = engineFlags{
	PassThroughSwitches: []string{
		"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
		"rules", "rules-enabled", "version",
	},
	FrontDoorFlags: []string{
		"directory", "lint-config", "verbose", "json", "phases", "coverage", "profile", "open",
	},
}

// runGoEngine checks a Go workspace or module by running this binary as the Go engine on it and rendering
// its records.
//
// A child of this same executable rather than a call in process, for the reason the Swift engine is a
// child: a panic or an out-of-memory in a full-syntax load ends the child, and the front door is left to
// say that nothing was checked. And not a separate binary, because one shipped beside cohere would need its
// own resolution and packaging path, where `os.Executable()` already names an engine whose provenance is
// this cohere's own.
func runGoEngine(location projectLocation, given map[string]bool, positionals []string) (int, error) {
	if given["tsconfig"] {
		return 1, fmt.Errorf(
			"--tsconfig names a TypeScript program, and %s is a Go %s, so there is no tsconfig to use",
			location.Root, goContainer(location.Root))
	}
	language := goLanguage(goContainer(location.Root))
	arguments, mode, argumentsError := engineArguments(language, goFlags, location, given, flagValue, positionals)
	if argumentsError != nil {
		return 1, argumentsError
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		return 1, fmt.Errorf("finding this cohere's own executable, which runs as the Go engine: %w", executableError)
	}

	run := newEngineRun(os.Stdout, mode, language, location.rootNote(), processStart)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	run.requested = engineRequestedPhases(given, flagValue)
	return runEngineBinary(executable, append([]string{goEngineVerb}, arguments...), run, os.Stderr)
}

// goContainer is what a Go root is called in the run's words: a workspace when it holds a go.work, which is
// then the build go runs there, and otherwise a module.
func goContainer(root string) string {
	if isRegularFile(filepath.Join(root, goWorkspaceMarker)) {
		return "workspace"
	}
	return "module"
}
