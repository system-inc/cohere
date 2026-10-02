package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/system-inc/cohere/internal/release/dispatch"
)

// swiftPassThroughSwitches are the boolean flags the Swift engine reads with the meaning the contract
// gives them, forwarded only when the caller typed them. The rest are refused by name, by the engine
// or below, because a flag accepted and ignored reads as a run that did what was asked.
var swiftPassThroughSwitches = []string{
	"no-fix", "fix", "types", "lint", "format", "format-all", "changed", "single-threaded",
	"rules", "rules-enabled", "version", "unused", "unused-all", "unused-deep", "timing",
}

// runSwiftEngine checks a Swift package by running cohere-swift on it and rendering its records.
//
// The engine is resolved the way the launcher resolves cohere: from this module's `swift/` sources,
// rebuilt when they or the toolchain change. An installed cohere with no source checkout has no Swift
// engine yet, and says so rather than checking nothing.
func runSwiftEngine(location projectLocation, given map[string]bool, positionals []string) (int, error) {
	if given["tsconfig"] {
		return 1, fmt.Errorf(
			"--tsconfig names a TypeScript program, and %s is a Swift package (it holds a %s), so there is no tsconfig to use",
			location.Root, swiftProjectMarker)
	}

	arguments, mode, err := swiftEngineArguments(location, given, flagValue, positionals)
	if err != nil {
		return 1, err
	}

	binaryPath, err := resolveSwiftEngineBinary(location)
	if err != nil {
		return 1, err
	}

	run := newSwiftRun(os.Stdout, mode, location.rootNote(), processStart)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	return runEngineBinary(binaryPath, arguments, run, os.Stderr)
}

// resolveSwiftEngineBinary picks the engine: the one the caller named, or the one this module's
// sources build.
func resolveSwiftEngineBinary(location projectLocation) (string, error) {
	if named := os.Getenv(dispatch.SwiftEngineOverrideVariable); named != "" {
		if !isRegularFile(named) {
			return "", fmt.Errorf("%s names %s, which is not a file, so nothing was checked", dispatch.SwiftEngineOverrideVariable, named)
		}
		fmt.Fprintf(os.Stderr, "cohere: running the Swift engine %s named by %s, not one built from the sources on disk\n",
			named, dispatch.SwiftEngineOverrideVariable)
		return named, nil
	}

	moduleDirectory, err := dispatch.FindModuleDirectory()
	if err != nil {
		return "", fmt.Errorf("%s is a Swift package, and the Swift engine is built from a cohere checkout: %w", location.Root, err)
	}
	binaryPath, _, err := dispatch.ResolveSwiftEngine(dispatch.DefaultPaths(moduleDirectory))
	return binaryPath, err
}

// flagValue reads a parsed flag's value by name.
func flagValue(name string) string {
	if found := flag.Lookup(name); found != nil {
		return found.Value.String()
	}
	return ""
}

// swiftEngineArguments builds the engine's command line, `--contract 1 --root <root> [flags] [paths]`,
// and says which question the run answers.
//
// Every path is made absolute against where the caller typed it, so the engine never has to know what
// `--directory` meant. Takes the flag reader as a parameter so a test can state the flags it means
// without touching the process's own.
func swiftEngineArguments(
	location projectLocation,
	given map[string]bool,
	value func(string) string,
	positionals []string,
) ([]string, swiftMode, error) {
	for _, refused := range []string{"explain"} {
		if given[refused] {
			// Refused here rather than forwarded for the engine to refuse, because its argument is a
			// path and the engine's sentence would name the path rather than the flag.
			return nil, "", fmt.Errorf("--%s is not implemented for Swift yet, so nothing was checked", refused)
		}
	}

	arguments := []string{"--contract", fmt.Sprint(swiftContractVersion), "--root", location.Root}
	for _, name := range swiftPassThroughSwitches {
		if given[name] && value(name) == "true" {
			arguments = append(arguments, "--"+name)
		}
	}
	if given["lint-config"] {
		arguments = append(arguments, "--lint-config", location.LintConfigFileName)
	}
	if given["fix-passes"] {
		arguments = append(arguments, "--fix-passes", value("fix-passes"))
	}
	for _, positional := range positionals {
		arguments = append(arguments, absoluteFrom(location.ArgumentBase, positional))
	}

	// The same precedence the TypeScript run gives these, so one command line asks one question
	// whichever engine answers it.
	mode := swiftModeCheck
	switch {
	case given["rules"] && value("rules") == "true":
		mode = swiftModeRules
	case given["rules-enabled"] && value("rules-enabled") == "true":
		mode = swiftModeRulesEnabled
	case given["version"] && value("version") == "true":
		mode = swiftModeVersion
	}
	return arguments, mode, nil
}

// runEngineBinary runs an engine, renders its stdout record by record as it arrives, passes its
// stderr through untouched, and returns the exit code the front door owns.
//
// Records are rendered as they stream so a slow types phase shows its fix line first, as a TypeScript
// run does. A record that breaks the contract stops the run there: the engine is killed, the phase
// line says what was not reached, and the error says the engine is broken.
func runEngineBinary(binaryPath string, arguments []string, run *swiftRun, standardError io.Writer) (int, error) {
	command := exec.Command(binaryPath, arguments...)
	command.Stderr = standardError
	standardOutput, err := command.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("connecting to the Swift engine's output: %w", err)
	}
	if err := command.Start(); err != nil {
		return 1, fmt.Errorf("starting the Swift engine %s: %w", binaryPath, err)
	}

	scanner := bufio.NewScanner(standardOutput)
	// A finding's message is bounded by what a rule writes, not by a line length, so the limit is far
	// past any real record and a line past it is still refused loudly rather than split.
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)

	var protocolError error
	for scanner.Scan() {
		if protocolError = run.accept(scanner.Bytes()); protocolError != nil {
			break
		}
	}
	if protocolError == nil && scanner.Err() != nil {
		protocolError = fmt.Errorf("reading its output: %w", scanner.Err())
	}

	if protocolError != nil {
		// Killed rather than drained: nothing it writes after breaking the contract can be trusted, and
		// waiting for it to finish would make the reader wait for output that will be discarded.
		_ = command.Process.Kill()
		_ = command.Wait()
		run.writeUnfinished("the Swift engine broke its contract")
		return 1, fmt.Errorf("the Swift engine is broken, so nothing it reported can be trusted: %w", protocolError)
	}

	waitError := command.Wait()
	exitCode, ended := describeExit(command.ProcessState, waitError)
	return run.finish(exitCode, ended)
}

// describeExit turns how a process ended into an exit code and the words for it.
//
// A process killed by a signal has no exit code, and Go reports -1 for it. Printing "exited -1" would
// state a number the process never chose, so a signal is named as a signal.
func describeExit(state *os.ProcessState, waitError error) (int, string) {
	if state == nil {
		return -1, fmt.Sprintf("could not be waited for (%v)", waitError)
	}
	if status, isUnix := state.Sys().(syscall.WaitStatus); isUnix && status.Signaled() {
		return -1, fmt.Sprintf("was killed by %s", status.Signal())
	}
	var exitError *exec.ExitError
	if waitError != nil && !errors.As(waitError, &exitError) {
		return -1, fmt.Sprintf("could not be waited for (%v)", waitError)
	}
	return state.ExitCode(), fmt.Sprintf("exited %d", state.ExitCode())
}
