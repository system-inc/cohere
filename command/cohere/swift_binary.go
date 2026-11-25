package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/system-inc/cohere/internal/command_line/sentence"
	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
)

// swiftFlags are the flags a Swift run takes. The pass-through switches are the ones cohere-swift reads
// with the meaning the contract gives them. The front door answers the rest itself, forwarding the lint
// config and fix passes with their values. Any flag given that is in neither list is refused by name (see
// contractArguments). It used to be dropped, and `--format-only` dropped meant a format gate that type-checked
// and linted the whole package, minutes of compiling under a verdict that said formatting (#nqb3mjv). The
// child has read `--format-only` and `--no-format` with TypeScript's meaning since c024df4b, so both pass
// through. The child reads `--no-cache` since 9def2dc1: every phase from source, in a scratch of its own that
// it removes. `--no-system-cache` is cohere-go's, whose system cache the child has no counterpart of, so the
// front door accepts it and forwards nothing, until the child keeps one.
var swiftFlags = contractFlags{
	PassThroughSwitches: []string{
		"no-fix", "fix", "types", "lint", "format", "format-all", "format-only", "no-format", "single-threaded",
		"rules", "rules-enabled", "version", "unused", "unused-all", "unused-deep", "timing", "no-cache",
	},
	FrontDoorFlags: []string{
		"directory", "lint-config", "fix-passes", "verbose", "json", "phases", "coverage", "profile", "open",
		"duplicates", "no-system-cache", "languages", "formats",
	},
}

// runSwiftProject checks a Swift package by running cohere-swift on it and rendering its records.
//
// A released cohere runs the child its package ships beside it. A development cohere resolves one the
// way the launcher resolves cohere: from this module's `swift_language/` sources, rebuilt when they or the
// toolchain change. See resolveSwiftBinary for the order.
func runSwiftProject(location projectLocation, given map[string]bool, positionals []string) (int, error) {
	if given["tsconfig"] {
		return 1, fmt.Errorf(
			"--tsconfig names a TypeScript program, and %s is a Swift package (it holds a %s), so there is no tsconfig to use",
			location.Root, swiftProjectMarker)
	}

	arguments, mode, contractArgumentsError := contractArguments(
		swiftLanguage,
		swiftFlags,
		location,
		given,
		flagValue,
		positionals,
	)
	if contractArgumentsError != nil {
		return 1, contractArgumentsError
	}
	// The front door profiles its own process to the file, and cohere-swift takes no `--profile` (swiftFlags has
	// no ProfileName), so the run says no Swift profile was written rather than leave the caller to find none, or an
	// empty file that reads as a profile of nothing (#r33n96f).
	if given["profile"] && flagValue("profile") != "" {
		fmt.Fprintf(streams.Errors, "cohere: %s\n", sentence.Of(fmt.Sprintf(
			"cohere-swift takes no --profile, so %s profiles the front door's process alone, and no Swift "+
				"profile was written", flagValue("profile"))))
	}

	// Before the child is resolved, so a run with nothing changed replays here and exits having hashed no child
	// source, asked no toolchain and started no process. Only a check: a listing is no verdict, and none of its flags
	// is one the run cache admits anyway.
	binaryChoice := swiftBinaryChoice(os.Getenv(dispatch.SwiftBinaryOverrideVariable), release.Current())
	if mode == contractModeCheck {
		beginSwiftRunCache(location)
	}

	// Building the child is a step the launcher's progress shows: in full under --verbose, otherwise one
	// line on a terminal, cleared before the report (#ytqqv8v).
	// The progress writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	dispatch.Report.SetVerbose(given["verbose"] && flagValue("verbose") == "true")
	binaryPath, sourceCommit, resolveSwiftBinaryError := resolvedSwiftBinary(location)
	dispatch.Report.Clear()
	if resolveSwiftBinaryError != nil {
		return 1, resolveSwiftBinaryError
	}

	run := swiftContractRun(streams.Output, mode, location)
	run.details = given["coverage"] && flagValue("coverage") == "true"
	run.swiftSourceCommit = sourceCommit
	run.requested = contractRequestedPhases(given, flagValue)
	// The front door answers --duplicates for Swift, whose child has no report of it: the run checks as it would, and
	// its report says it did not run, so a root holding Swift beside TypeScript or Go still reports theirs.
	if given["duplicates"] && flagValue("duplicates") == "true" {
		run.duplicatesUnanswered = "cohere-swift has no duplicates report; it reads TypeScript and Go functions"
	}
	// What this run leaves to the Swift projects below it, when it is one project of several (see
	// discovery.swiftYield). Resolved once here, as the child's paths are resolved against it.
	yield, activeProjectYieldError := activeProjectYield()
	if activeProjectYieldError != nil {
		return 1, activeProjectYieldError
	}
	for _, directory := range yield.Directories {
		run.yieldedDirectories = append(run.yieldedDirectories, resolvedPath(directory))
	}
	// os.Stderr is read here, after the run cache began, so the child writes into the recording's tee.
	exitCode, runError := runContractBinary(binaryPath, arguments, run, os.Stderr)
	// A test instrument: a file written after the child read the tree and before its run is recorded, holding
	// COHERE_TEST_WRITE_AFTER_SWIFT_CHILD_TEXT, so a test can change an input the verdict was read from. See
	// TestASwiftInputChangedDuringTheRunIsNeverReplayedOver.
	if written := os.Getenv("COHERE_TEST_WRITE_AFTER_SWIFT_CHILD"); written != "" {
		if createDirectoryError := os.MkdirAll(filepath.Dir(written), 0o755); createDirectoryError != nil {
			return 1, createDirectoryError
		}
		text := []byte(os.Getenv("COHERE_TEST_WRITE_AFTER_SWIFT_CHILD_TEXT"))
		if writeError := os.WriteFile(written, text, 0o644); writeError != nil {
			return 1, writeError
		}
	}
	if runError == nil {
		declareSwiftRunCacheInputs(run, exitCode, location.LintConfigFileName, binaryPath, binaryChoice)
	}
	return exitCode, runError
}

// swiftContractRun is a Swift run timed from its check's start, so a build of the child before it is said apart
// from the check's time (program_build.go).
func swiftContractRun(out *bufio.Writer, mode contractMode, location projectLocation) *contractRun {
	return newContractRun(out, mode, swiftLanguage, location.rootNote(), checkStart())
}

// resolveDevelopmentSwiftBinary resolves a development cohere's cohere-swift, building it when there is none. A
// variable so a test can stand in for a build, which takes minutes.
var resolveDevelopmentSwiftBinary = dispatch.ResolveSwiftBinary

// swiftBinaryPlatform is the platform this cohere runs on. A variable so a test can run as a platform
// the machine running it is not.
var swiftBinaryPlatform = release.Target{GoOperatingSystem: runtime.GOOS, GoArchitecture: runtime.GOARCH}

// resolveSwiftBinary picks the child, in this order:
//
//  1. The one the caller named with COHERE_SWIFT_BINARY, on any platform and any build.
//  2. None, on a platform the child does not ship for, refused by name before anything is looked for.
//     A Linux checkout would otherwise try to build the child and bury the reason in a toolchain error.
//  3. For a released cohere, the child its package ships beside it, and nothing else: see
//     shippedSwiftBinary.
//  4. For a development cohere, one built from this module's checkout, below.
//
// A development cohere never takes a child from beside itself. A `go build -o` next to a stale
// cohere-swift would run that cohere-swift silently, and the contract version cannot catch rules that drifted
// inside one version. So each kind of build has one source: a release runs its sibling, development runs
// the checkout.
//
// A cohere binary that names its commit and has nothing uncommitted in it, which is what the launcher
// builds for every gate, gets a child built from that same commit. Any other cohere, a `--dev` build
// from a dirty tree included, gets one built from the working tree, so a development run checks
// development rules in both languages and says so through both provenance records.
//
// The second value is the commit whose Swift sources the child is guaranteed to match, or empty when
// nothing guarantees it: a child named by the override, or one built from the working tree. The
// provenance is this cohere's, passed in rather than read here so a test can state a clean named commit,
// which a test binary never has, and prove the override still vouches for none.
func resolveSwiftBinary(location projectLocation, provenance release.Provenance) (string, string, error) {
	if named := os.Getenv(dispatch.SwiftBinaryOverrideVariable); named != "" {
		if !isRegularFile(named) {
			return "", "", fmt.Errorf(
				"%s names %s, which is not a file, so nothing was checked",
				dispatch.SwiftBinaryOverrideVariable,
				named,
			)
		}
		// The invocation's, as a graph line is (see invocationTag): a replay ran no child, named or not.
		printInvocation(
			streams.Errors,
			"cohere: running cohere-swift %s named by %s, not one built from the sources on disk\n",
			named,
			dispatch.SwiftBinaryOverrideVariable,
		)
		// Asked for the child's sake, as for a shipped one below.
		// Ignored: a toolchain that does not answer is the child's to report.
		_, _ = swiftVersionOutput()
		return named, "", nil
	}

	if !release.ShipsSwiftBinary(swiftBinaryPlatform.GoOperatingSystem) {
		return "", "", fmt.Errorf(
			"Swift is not available on this platform (%s): %s ships only for macOS, so nothing in %s was checked",
			swiftBinaryPlatform,
			release.SwiftBinaryFileName,
			location.Root,
		)
	}

	if !provenance.IsDevelopment() {
		// Asked for the children' sake, which read it rather than each asking again (see swift_toolchain.go).
		// Ignored: a toolchain that does not answer is the child's to report, as it was before this asked.
		_, _ = swiftVersionOutput()
		binaryPath, shippedSwiftBinaryError := shippedSwiftBinary(provenance)
		return binaryPath, "", shippedSwiftBinaryError
	}

	moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
	if findModuleDirectoryError != nil {
		return "", "", fmt.Errorf(
			"%s is a Swift package, and cohere-swift is built from a cohere checkout: %w",
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
	binaryPath, built, resolveSwiftBinaryError := resolveDevelopmentSwiftBinary(
		dispatch.DefaultPaths(moduleDirectory),
		commit,
		contractVersion,
		versionOutput,
	)
	if resolveSwiftBinaryError != nil {
		return "", "", resolveSwiftBinaryError
	}
	if built {
		recordProgramBuild(swiftProgramName, binaryPath, time.Since(started))
	}
	return binaryPath, commit, nil
}

// shippedSwiftBinary is the child a released cohere's package ships beside it: release.SwiftBinaryFileName
// in the directory of this executable, symlinks resolved, which is the package's `bin/`, since the npm
// launcher execs the platform binary directly.
//
// It never falls back to a checkout. A release run inside a cohere clone would otherwise build whatever
// rules were on disk there and report them under the release's version. A missing sibling is a broken
// install, and it says so rather than checking nothing. It vouches for no commit: the release stamps
// none into cohere, and the child's own provenance record names what it was built from.
func shippedSwiftBinary(provenance release.Provenance) (string, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return "", fmt.Errorf(
			"finding this cohere's own executable, beside which its cohere-swift ships: %w",
			executableError,
		)
	}
	if resolved, resolveSymlinksError := filepath.EvalSymlinks(executable); resolveSymlinksError == nil {
		executable = resolved
	}
	binaryPath := filepath.Join(filepath.Dir(executable), release.SwiftBinaryFileName)
	if !isRegularFile(binaryPath) {
		return "", fmt.Errorf(
			"this cohere %s install has no cohere-swift: expected %s, which the macOS package ships beside cohere, so nothing was checked; reinstall cohere",
			provenance.Version,
			binaryPath,
		)
	}
	return binaryPath, nil
}

// committedSwiftSource is the commit cohere-swift is built from, or empty for the working tree.
//
// Only a cohere that names its commit and was built from a tree with nothing uncommitted in it has a
// commit to offer: anything else could not be reproduced from one, so neither could a child built to
// match it. That empty answer is also what keeps `cohere --version` from claiming a commit for a child
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
