package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/streams"
)

// swiftFlags are the flags a Swift run takes. The pass-through switches are the ones the Swift engine reads
// with the meaning the contract gives them. The front door answers the rest itself, forwarding the lint
// config and fix passes with their values. Any flag given that is in neither list is refused by name (see
// engineArguments). It used to be dropped, and `--format-only` dropped meant a format gate that type-checked
// and linted the whole package, minutes of compiling under a verdict that said formatting (#nqb3mjv). The
// engine has read `--format-only` and `--no-format` with TypeScript's meaning since c024df4b, so both pass
// through. The engine reads `--no-cache` since 9def2dc1: every phase from source, in a scratch of its own that
// it removes.
var swiftFlags = engineFlags{
	PassThroughSwitches: []string{
		"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
		"rules", "rules-enabled", "version", "unused", "unused-all", "unused-deep", "timing", "no-cache",
	},
	FrontDoorFlags: []string{
		"directory", "lint-config", "fix-passes", "verbose", "json", "phases", "coverage", "profile", "open",
	},
}

// runSwiftEngine checks a Swift package by running cohere-swift on it and rendering its records.
//
// A released cohere runs the engine its package ships beside it. A development cohere resolves one the
// way the launcher resolves cohere: from this module's `swift/` sources, rebuilt when they or the
// toolchain change. See resolveSwiftEngineBinary for the order.
func runSwiftEngine(location projectLocation, given map[string]bool, positionals []string) (int, error) {
	if given["tsconfig"] {
		return 1, fmt.Errorf(
			"--tsconfig names a TypeScript program, and %s is a Swift package (it holds a %s), so there is no tsconfig to use",
			location.Root, swiftProjectMarker)
	}

	arguments, mode, engineArgumentsError := engineArguments(
		swiftLanguage,
		swiftFlags,
		location,
		given,
		flagValue,
		positionals,
	)
	if engineArgumentsError != nil {
		return 1, engineArgumentsError
	}

	// Before the engine is resolved, so a run with nothing changed replays here and exits having hashed no engine
	// source, asked no toolchain and started no process. Only a check: a listing is no verdict, and none of its flags
	// is one the run cache admits anyway.
	engineChoice := swiftEngineChoice(os.Getenv(dispatch.SwiftEngineOverrideVariable), release.Current())
	if mode == engineModeCheck {
		beginSwiftRunCache(location)
	}

	// Building the engine is a step the launcher's progress shows: in full under --verbose, otherwise one
	// line on a terminal, cleared before the report (#ytqqv8v).
	// The progress writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	dispatch.Report.SetVerbose(given["verbose"] && flagValue("verbose") == "true")
	binaryPath, sourceCommit, resolveSwiftEngineBinaryError := resolvedSwiftEngineBinary(location)
	dispatch.Report.Clear()
	if resolveSwiftEngineBinaryError != nil {
		return 1, resolveSwiftEngineBinaryError
	}

	run := swiftEngineRun(streams.Output, mode, location)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	run.engineSourceCommit = sourceCommit
	run.requested = engineRequestedPhases(given, flagValue)
	// What this run leaves to the Swift projects below it, when it is one project of several (see
	// discovery.swiftYield). Resolved once here, as the engine's paths are resolved against it.
	yield, activeProjectYieldError := activeProjectYield()
	if activeProjectYieldError != nil {
		return 1, activeProjectYieldError
	}
	for _, directory := range yield.Directories {
		run.yieldedDirectories = append(run.yieldedDirectories, resolvedPath(directory))
	}
	// os.Stderr is read here, after the run cache began, so the engine writes into the recording's tee.
	exitCode, runError := runEngineBinary(binaryPath, arguments, run, os.Stderr)
	// A test instrument: a file written after the engine read the tree and before its run is recorded, holding
	// COHERE_TEST_WRITE_AFTER_SWIFT_ENGINE_TEXT, so a test can change an input the verdict was read from. See
	// TestASwiftInputChangedDuringTheRunIsNeverReplayedOver.
	if written := os.Getenv("COHERE_TEST_WRITE_AFTER_SWIFT_ENGINE"); written != "" {
		if createDirectoryError := os.MkdirAll(filepath.Dir(written), 0o755); createDirectoryError != nil {
			return 1, createDirectoryError
		}
		text := []byte(os.Getenv("COHERE_TEST_WRITE_AFTER_SWIFT_ENGINE_TEXT"))
		if writeError := os.WriteFile(written, text, 0o644); writeError != nil {
			return 1, writeError
		}
	}
	if runError == nil {
		declareSwiftRunCacheInputs(run, exitCode, location.LintConfigFileName, binaryPath, engineChoice)
	}
	return exitCode, runError
}

// swiftEngineRun is a Swift run timed from its check's start, so a build of the engine before it is said apart
// from the check's time (engine_build.go).
func swiftEngineRun(out *bufio.Writer, mode engineMode, location projectLocation) *engineRun {
	return newEngineRun(out, mode, swiftLanguage, location.rootNote(), checkStart())
}

// resolveDevelopmentSwiftEngine resolves a development cohere's engine, building it when there is none. A
// variable so a test can stand in for a build, which takes minutes.
var resolveDevelopmentSwiftEngine = dispatch.ResolveSwiftEngine

// swiftEnginePlatform is the platform this cohere runs on. A variable so a test can run as a platform
// the machine running it is not.
var swiftEnginePlatform = release.Target{GoOperatingSystem: runtime.GOOS, GoArchitecture: runtime.GOARCH}

// resolveSwiftEngineBinary picks the engine, in this order:
//
//  1. The one the caller named with COHERE_SWIFT_ENGINE, on any platform and any build.
//  2. None, on a platform the engine does not ship for, refused by name before anything is looked for.
//     A Linux checkout would otherwise try to build the engine and bury the reason in a toolchain error.
//  3. For a released cohere, the engine its package ships beside it, and nothing else: see
//     shippedSwiftEngine.
//  4. For a development cohere, one built from this module's checkout, below.
//
// A development cohere never takes an engine from beside itself. A `go build -o` next to a stale
// cohere-swift would run that engine silently, and the contract version cannot catch rules that drifted
// inside one version. So each kind of build has one source: a release runs its sibling, development runs
// the checkout.
//
// A cohere binary that names its commit and has nothing uncommitted in it, which is what the launcher
// builds for every gate, gets an engine built from that same commit. Any other cohere, a `--dev` build
// from a dirty tree included, gets one built from the working tree, so a development run checks
// development rules in both engines and says so through both provenance records.
//
// The second value is the commit whose Swift sources the engine is guaranteed to match, or empty when
// nothing guarantees it: an engine named by the override, or one built from the working tree. The
// provenance is this cohere's, passed in rather than read here so a test can state a clean named commit,
// which a test binary never has, and prove the override still vouches for none.
func resolveSwiftEngineBinary(location projectLocation, provenance release.Provenance) (string, string, error) {
	if named := os.Getenv(dispatch.SwiftEngineOverrideVariable); named != "" {
		if !isRegularFile(named) {
			return "", "", fmt.Errorf(
				"%s names %s, which is not a file, so nothing was checked",
				dispatch.SwiftEngineOverrideVariable,
				named,
			)
		}
		// The invocation's, as a graph line is (see invocationTag): a replay ran no engine, named or not.
		printInvocation(
			streams.Errors,
			"cohere: running the Swift engine %s named by %s, not one built from the sources on disk\n",
			named,
			dispatch.SwiftEngineOverrideVariable,
		)
		// Asked for the engine's sake, as for a shipped one below.
		// Ignored: a toolchain that does not answer is the engine's to report.
		_, _ = swiftVersionOutput()
		return named, "", nil
	}

	if !release.ShipsSwiftEngine(swiftEnginePlatform.GoOperatingSystem) {
		return "", "", fmt.Errorf(
			"Swift is not available on this platform (%s): %s ships only for macOS, so nothing in %s was checked",
			swiftEnginePlatform,
			release.SwiftEngineFileName,
			location.Root,
		)
	}

	if !provenance.IsDevelopment() {
		// Asked for the engines' sake, which read it rather than each asking again (see swift_toolchain.go).
		// Ignored: a toolchain that does not answer is the engine's to report, as it was before this asked.
		_, _ = swiftVersionOutput()
		binaryPath, shippedSwiftEngineError := shippedSwiftEngine(provenance)
		return binaryPath, "", shippedSwiftEngineError
	}

	moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
	if findModuleDirectoryError != nil {
		return "", "", fmt.Errorf(
			"%s is a Swift package, and the Swift engine is built from a cohere checkout: %w",
			location.Root,
			findModuleDirectoryError,
		)
	}
	commit := committedSwiftSource(provenance)
	versionOutput, swiftVersionOutputError := swiftVersionOutput()
	if swiftVersionOutputError != nil {
		return "", "", swiftVersionOutputError
	}
	started := time.Now()
	binaryPath, built, resolveSwiftEngineError := resolveDevelopmentSwiftEngine(
		dispatch.DefaultPaths(moduleDirectory),
		commit,
		engineContractVersion,
		versionOutput,
	)
	if resolveSwiftEngineError != nil {
		return "", "", resolveSwiftEngineError
	}
	if built {
		recordEngineBuild(swiftEngineName, binaryPath, time.Since(started))
	}
	return binaryPath, commit, nil
}

// shippedSwiftEngine is the engine a released cohere's package ships beside it: release.SwiftEngineFileName
// in the directory of this executable, symlinks resolved, which is the package's `bin/`, since the npm
// launcher execs the platform binary directly.
//
// It never falls back to a checkout. A release run inside a cohere clone would otherwise build whatever
// rules were on disk there and report them under the release's version. A missing sibling is a broken
// install, and it says so rather than checking nothing. It vouches for no commit: the release stamps
// none into cohere, and the engine's own provenance record names what it was built from.
func shippedSwiftEngine(provenance release.Provenance) (string, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return "", fmt.Errorf(
			"finding this cohere's own executable, beside which its Swift engine ships: %w",
			executableError,
		)
	}
	if resolved, resolveSymlinksError := filepath.EvalSymlinks(executable); resolveSymlinksError == nil {
		executable = resolved
	}
	binaryPath := filepath.Join(filepath.Dir(executable), release.SwiftEngineFileName)
	if !isRegularFile(binaryPath) {
		return "", fmt.Errorf(
			"this cohere %s install has no Swift engine: expected %s, which the macOS package ships beside cohere, so nothing was checked; reinstall cohere",
			provenance.Version,
			binaryPath,
		)
	}
	return binaryPath, nil
}

// committedSwiftSource is the commit the Swift engine is built from, or empty for the working tree.
//
// Only a cohere that names its commit and was built from a tree with nothing uncommitted in it has a
// commit to offer: anything else could not be reproduced from one, so neither could an engine built to
// match it. That empty answer is also what keeps `cohere --version` from claiming a commit for an engine
// nothing vouches for.
func committedSwiftSource(provenance release.Provenance) string {
	if provenance.SelfCommit != "" && !provenance.SourceTreeModified {
		return provenance.SelfCommit
	}
	return ""
}

// flagValue reads a parsed flag's value by name.
func flagValue(name string) string {
	if found := flag.Lookup(name); found != nil {
		return found.Value.String()
	}
	return ""
}
