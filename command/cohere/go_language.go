package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/system-inc/cohere/internal/go_language"
	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

// goLanguageVerb is the hidden first argument that runs this binary as cohere-go rather than as the front
// door. Hidden because nobody types it: the front door runs it as a child, the way it runs cohere-swift.
const goLanguageVerb = "go-language"

// goChildVariable marks a process the front door started as its cohere-go.
//
// The child is this same binary, so a child that missed the `go-language` dispatch would read its command
// line as a front door's, find the same Go root, and start another child, without end: each generation a
// new process in a process group of its own, out of reach of any kill aimed at the first. So the front door
// marks its child, and a front door that finds the mark refuses to start a child, which makes the
// recursion impossible however the dispatch is broken.
const goChildVariable = "COHERE_GO_CHILD"

// runGoLanguageVerb is the hidden `go-language` mode, matched by main before anything else reads the command
// line: cohere-go, given everything after the verb, writing contract 3 to standardOutput for the front
// door that started it, or for the editor's save the file's text, from the buffer on standardInput (runGoStdin). The
// memory available is this process's own policy's reading, taken at init, which the child sizes its limit from
// (go_language's capMemory).
func runGoLanguageVerb(
	arguments []string,
	standardInput io.Reader,
	standardOutput io.Writer,
	standardError io.Writer,
) int {
	return go_language.Main(arguments, activeMemoryPolicy.available, standardInput, standardOutput, standardError)
}

// goFlags are the flags a Go run takes, as agreed with the child on #a0cgmh6. `lint-config` is forwarded
// with its absolute path, and the child reads that file's `go` block in place of the root's settings. Any
// flag in neither list is refused by name, among them the unused report, explain, the tsconfig and fix
// passes, none of which cohere-go implements yet.
//
// `no-cache` is the front door's as much as the child's: the run neither replays nor records (see
// beginGoRunCache), and it is forwarded so a cache the child keeps of its own is off too.
//
// `no-system-cache` is the child's alone (#syscache): it neither reads nor writes its system cache, the standard
// library's entries in the user's cache directory, which `no-cache` leaves on. The run cache admits it, since its key
// holds every argument, so a run under it never replays one without it, nor the reverse.
//
// `host-build-only` is Go's alone. Without it the child checks every build the project's files name beside the
// host's, so a file only another build compiles is checked and no gap (#72n9mq0, #qqyq61s); with it the child checks
// the host's build alone and names each such file as a gap. The run cache admits it, as its own record, since the key
// holds every argument, and a run without it is replayed only while every build's inputs are unchanged, since the
// child's inputs record names every build's load (#e4b08k7).
//
// `timing` asks the child for its timing record, what the run's CPU went to, which the front door prints as
// TypeScript's timing table (#3apgjer). The run cache never admits it (see runCacheAdmits).
//
// `timeline` is Go's alone too: the child's timeline record, when each phase and each build began and ended on the
// run's clock, which the front door prints as a table (#3v2zp7g). It changes nothing of the run, but a replay would
// print another run's clock, so the run cache never admits it either.
//
// `stdin-filepath` is the editor's save (#qtna8m4), forwarded with its absolute path, and run by runGoStdin rather
// than as a check: the child reads the buffer on stdin and writes the file's text where a check writes records.
//
// `profile` is the front door's CPU profile of its own process, and the child writes its own beside it, named `go`
// (contractProfilePath), since the run's work is the child's and a profile of the front door alone shows it waiting
// (#r33n96f). The run cache never admits it, so a profiled run is always one the child ran.
var goFlags = contractFlags{
	PassThroughSwitches: []string{
		"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
		"rules", "rules-enabled", "version", "no-cache", "no-system-cache", "host-build-only", "timing", "timeline",
		"duplicates",
	},
	FrontDoorFlags: []string{
		"directory", "lint-config", "verbose", "json", "phases", "coverage", "profile", "open", "stdin-filepath",
		"languages", "formats",
	},
	ProfileName: "go",
}

// runGoProject checks a Go workspace or module by running this binary as cohere-go on it and rendering
// its records.
//
// A child of this same executable rather than a call in process, for the reason cohere-swift is a
// child: a panic or an out-of-memory in a full-syntax load ends the child, and the front door is left to
// say that nothing was checked. And not a separate binary, because one shipped beside cohere would need its
// own resolution and packaging path, where `os.Executable()` already names a child whose provenance is
// this cohere's own.
func runGoProject(location projectLocation, given map[string]bool, positionals []string) (int, error) {
	if os.Getenv(goChildVariable) != "" {
		// Exit 2, the contract's "did not run", so the front door above reports nothing checked.
		fmt.Fprintf(streams.Errors, "cohere: %s\n", sentence.Of(fmt.Sprintf(
			"this process was started as cohere-go (%s is set) and was about to start another, "+
				"so it refused, and nothing was checked", goChildVariable)))
		showWarning()
		return 2, nil
	}
	if given["tsconfig"] {
		return 1, fmt.Errorf(
			"--tsconfig names a TypeScript program, and %s is a Go %s, so there is no tsconfig to use",
			location.Root, goContainer(location.Root))
	}
	if given["stdin-filepath"] {
		return runGoStdin(location, given, positionals, os.Stdin, os.Stdout)
	}
	language := goLanguage(goContainer(location.Root))
	arguments, mode, argumentsError := contractArguments(language, goFlags, location, given, flagValue, positionals)
	if argumentsError != nil {
		return 1, argumentsError
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		return 1, fmt.Errorf("finding this cohere's own executable, which runs as cohere-go: %w", executableError)
	}

	// Before the child is started, so a run with nothing changed replays here and exits without one. Only a
	// check: a listing is no verdict, and none of its flags is one the run cache admits anyway.
	if mode == contractModeCheck {
		beginGoRunCache(location)
	}

	run := newContractRun(streams.Output, mode, language, location.rootNote(), processStart)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	run.requested = contractRequestedPhases(given, flagValue)
	run.timingRequested = given["timing"] && flagValue("timing") == "true"
	run.timelineRequested = given["timeline"] && flagValue("timeline") == "true"
	run.duplicatesRequested = given["duplicates"] && flagValue("duplicates") == "true"
	run.childEnvironment = []string{goChildVariable + "=1"}
	// os.Stderr is read here, after the run cache began, so the child writes into the recording's tee.
	exitCode, runError := runContractBinary(executable, append([]string{goLanguageVerb}, arguments...), run, os.Stderr)
	// A test instrument: a file written after the child read the tree and before its run is recorded, holding
	// COHERE_TEST_WRITE_AFTER_GO_CHILD_TEXT, so a test can change an input the verdict was read from. See
	// TestAGoInputChangedDuringTheRunIsNeverReplayedOver.
	if written := os.Getenv("COHERE_TEST_WRITE_AFTER_GO_CHILD"); written != "" {
		if createDirectoryError := os.MkdirAll(filepath.Dir(written), 0o755); createDirectoryError != nil {
			return 1, createDirectoryError
		}
		text := []byte(os.Getenv("COHERE_TEST_WRITE_AFTER_GO_CHILD_TEXT"))
		if writeError := os.WriteFile(written, text, 0o644); writeError != nil {
			return 1, writeError
		}
	}
	if runError == nil {
		declareGoRunCacheInputs(run, exitCode, location.LintConfigFileName)
	}
	return exitCode, runError
}

// goContainer is what a Go root is called in the run's words: a workspace when it holds a go.work, which is
// then the build go runs there, and otherwise a module.
func goContainer(root string) string {
	if isRegularFile(filepath.Join(root, goWorkspaceMarker)) {
		return "workspace"
	}
	return "module"
}
