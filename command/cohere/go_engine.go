package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/goengine"
	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

// goEngineVerb is the hidden first argument that runs this binary as the Go engine rather than as the front
// door. Hidden because nobody types it: the front door runs it as a child, the way it runs cohere-swift.
const goEngineVerb = "go-engine"

// goEngineChildVariable marks a process the front door started as its Go engine.
//
// The engine is this same binary, so a child that missed the `go-engine` dispatch would read its command
// line as a front door's, find the same Go root, and start another engine, without end: each generation a
// new process in a process group of its own, out of reach of any kill aimed at the first. So the front door
// marks its child, and a front door that finds the mark refuses to start an engine, which makes the
// recursion impossible however the dispatch is broken.
const goEngineChildVariable = "COHERE_GO_ENGINE_CHILD"

// runGoEngineVerb is the hidden `go-engine` mode, matched by main before anything else reads the command
// line: the Go engine, given everything after the verb, writing contract 3 to standardOutput for the front
// door that started it.
func runGoEngineVerb(arguments []string, standardOutput io.Writer, standardError io.Writer) int {
	return goengine.Main(arguments, standardOutput, standardError)
}

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
	if os.Getenv(goEngineChildVariable) != "" {
		// Exit 2, the contract's "did not run", so the front door above reports nothing checked.
		fmt.Fprintf(streams.Errors, "cohere: %s\n", sentence.Of(fmt.Sprintf(
			"this process was started as a Go engine (%s is set) and was about to start another, "+
				"so it refused, and nothing was checked", goEngineChildVariable)))
		showWarning()
		return 2, nil
	}
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

	run := newEngineRun(streams.Output, mode, language, location.rootNote(), processStart)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	run.requested = engineRequestedPhases(given, flagValue)
	run.engineEnvironment = []string{goEngineChildVariable + "=1"}
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
