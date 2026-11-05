// Command cohere-dispatch is the shim that `node_modules/.bin/cohere` runs.
//
// It decides which cohere binary should handle this invocation and execs it, so that the common
// case costs one stat and one exec. When a new commit has landed it rebuilds first, which is the
// price of compiling rules in rather than loading them, paid automatically instead of by hand. It
// builds from the checkout's committed tree; `--dev` builds the working tree and says so.
//
// Every flag it does not own is passed through untouched, because it stands in front of the real
// command rather than wrapping it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
	"github.com/system-inc/cohere/internal/sentence"
	"github.com/system-inc/cohere/internal/streams"
)

// main exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode runs the dispatcher and returns the code it exits with, after every path out has finished.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	if runError := run(); runError != nil {
		// The launcher's report writes to stderr itself, so what this process said first is delivered first.
		streams.Show(streams.Errors)
		dispatch.Report.Fail("cohere: %s", sentence.FromError(runError))
		return streams.Finish(1)
	}
	// Every path out finishes, this one too, so none can leave what was written in the buffers.
	return streams.Finish(0)
}

func run() error {
	// The dispatcher owns exactly four flags and forwards everything else. They are matched
	// positionally at the front rather than with the flag package, because flag.Parse would stop at
	// the first argument it does not recognize and swallow flags meant for the real binary.
	arguments := os.Args[1:]
	development := false
	verbose := false
	frozen := false
	wait := waitIsRequested()

	// The dispatcher's flags end at the first argument it does not own, which goes to cohere with the rest.
	ownFlags := true
	for ownFlags && len(arguments) > 0 {
		switch arguments[0] {
		case "--wait":
			wait = true
			arguments = arguments[1:]
		case "--dev":
			development = true
			arguments = arguments[1:]
		case "--dispatch-verbose":
			verbose = true
			arguments = arguments[1:]
		case "--frozen":
			frozen = true
			arguments = arguments[1:]
		default:
			ownFlags = false
		}
	}

	// The launcher's own lines print in full under its own flag or cohere's --verbose, which it forwards
	// rather than owns; otherwise a build is one line on a terminal that clears before cohere prints.
	dispatch.Report.SetVerbose(verbose || asksForVerbose(arguments))

	if frozen {
		binaryPath, resolveFrozenBinaryError := resolveFrozenBinary()
		if resolveFrozenBinaryError != nil {
			return resolveFrozenBinaryError
		}
		return execute(binaryPath, arguments, wait)
	}

	binaryPath, resolveBinaryError := resolveBinary(development, verbose)
	if resolveBinaryError != nil {
		return resolveBinaryError
	}

	// The launcher's report writes to stderr itself, so what resolving said is delivered before it clears.
	streams.Show(streams.Errors)
	dispatch.Report.Clear()
	return execute(binaryPath, arguments, wait)
}

// asksForVerbose reports whether the arguments forwarded to cohere ask for its --verbose view, in any of
// the spellings its flag package accepts.
func asksForVerbose(arguments []string) bool {
	for _, argument := range arguments {
		switch argument {
		case "--verbose", "-verbose", "--verbose=true", "-verbose=true":
			return true
		}
	}
	return false
}

// waitIsRequested reports whether the environment asks the dispatcher to wait for the engine to finish
// everything, its cache write included, rather than return when the verdict is out. CI always waits: a job
// that ends while its cache is still being written can lose the write to the runner tearing down.
func waitIsRequested() bool {
	if value := os.Getenv("COHERE_WAIT"); value != "" && value != "0" && value != "false" {
		return true
	}
	return os.Getenv("CI") == "true"
}

// execute runs the cohere binary and returns its exit code to the caller.
//
// Waiting, it replaces this process with the binary, as it always did: the real binary takes over, so it
// inherits the terminal directly and its exit code is the one the caller sees. A wrapper that forwarded
// the status would be one more layer able to lose a non-zero exit, and losing a non-zero exit is how a
// gate goes quietly green.
//
// Otherwise it returns early. The engine's report is the caller's answer, and after printing it the engine
// still records the run, writes its cache table and gives back a heap of several gigabytes, about 85ms on
// ahra that nobody needs to wait for (#zqsdzbq). So the binary runs as a child holding one more descriptor,
// and the moment it writes its exit code there, this process exits with that code while the child finishes
// alone. Every byte the caller reads still comes from the child's own inherited stdout and stderr. A child
// that exits without a verdict, crashing or killed, hands back its exit status as exec would have.
func execute(binaryPath string, arguments []string, wait bool) error {
	if wait || runtime.GOOS == "windows" {
		return replaceProcess(binaryPath, arguments)
	}

	return returnEarly(binaryPath, arguments)
}

// resolveFrozenBinary picks the newest cached binary and announces that it did.
//
// The announcement is the feature, not decoration. Freezing runs a binary whose inputs were never
// compared against the rules on disk, so the one thing that separates it from the stale-binary
// defect is that the operator is told: which binary, when it was built, and that it may not match.
// A stated limitation is not a lie; silence would be. This prints to stderr so it survives a caller
// piping stdout, and it prints before the exec because after the exec there is no "after".
func resolveFrozenBinary() (string, error) {
	moduleDirectory, findModuleDirectoryError := dispatch.FindModuleDirectory()
	if findModuleDirectoryError != nil {
		return "", findModuleDirectoryError
	}

	frozen, resolveFrozenError := dispatch.ResolveFrozen(dispatch.DefaultPaths(moduleDirectory))
	if resolveFrozenError != nil {
		return "", resolveFrozenError
	}

	fmt.Fprintf(streams.Errors, "cohere: --frozen, so the rules on disk were never checked against this binary.\n")
	fmt.Fprintf(streams.Errors, "  running: %s\n", filepath.Base(frozen.Path))
	fmt.Fprintf(streams.Errors, "  hash:    %s\n", frozen.Hash)
	fmt.Fprintf(streams.Errors, "  built:   %s\n", frozen.ModifiedAt)

	return frozen.Path, nil
}

// resolveBinary picks the binary to run.
//
// There are two worlds and the seam between them is the whole risk. In a source checkout with a Go
// toolchain, the committed rules are the truth and a stale binary is the defect, so the rebuild cache
// decides. On an installed machine there is no source and no toolchain, so the shipped platform
// binary is the only answer. Both halves are loud on failure; neither falls through to the other,
// because "rebuild what I cannot see" and "ship a binary I did not build" are each a way of running
// something other than what was asked for.
//
// In a checkout the committed tree is what gets built, not the files on disk. The checkout is shared,
// and a gate that built from disk ran whatever any member had half-written. `--dev` is the explicit
// way to run the working tree instead.
//
// The order is deliberate. An explicit override wins everywhere, including inside a source
// checkout, because someone who names a binary has stated what they want to run and a rebuild that
// quietly overrode them would produce results they would read as their build's.
func resolveBinary(development bool, verbose bool) (string, error) {
	if os.Getenv(release.BinaryOverrideVariable) != "" {
		return release.Resolve(nil)
	}

	moduleDirectory, moduleError := dispatch.FindModuleDirectory()

	// No module means an installed package: there is no source to build from, so the shipped
	// binary is the only option, and a missing one is an error naming the platform.
	if moduleError != nil {
		return resolveInstalled(moduleError)
	}

	if !dispatch.HasToolchain() {
		// Source is present but nothing can compile it. The shipped binary is still the right
		// answer, and it is still never a fallback to something that might do nothing.
		return resolveInstalled(dispatch.ErrNoToolchain)
	}

	paths := dispatch.DefaultPaths(moduleDirectory)

	if development {
		binaryPath, built, resolveWorkingTreeError := dispatch.ResolveWorkingTree(paths, "./command/cohere")
		if resolveWorkingTreeError != nil {
			return "", resolveWorkingTreeError
		}
		// On every run, not only when it rebuilds. The binary carries every member's uncommitted edits
		// in the shared checkout, and a reader of its findings has to know that before they read them.
		fmt.Fprintf(streams.Errors, "cohere: --dev runs the working tree in %s, uncommitted edits included "+
			"(anyone's, not only yours), so no commit reproduces these results\n", moduleDirectory)
		if built && verbose {
			fmt.Fprintf(streams.Errors, "cohere: working tree changed, rebuilt %s\n", filepath.Base(binaryPath))
		}
		return binaryPath, nil
	}

	binaryPath, commit, built, resolveCommittedError := dispatch.ResolveCommitted(paths, "./command/cohere")
	if resolveCommittedError != nil {
		return "", resolveCommittedError
	}
	if built && verbose {
		fmt.Fprintf(streams.Errors, "cohere: built %s from commit %s\n", filepath.Base(binaryPath), commit)
	}
	return binaryPath, nil
}

// resolveInstalled finds the shipped platform binary, reporting why building was not an option.
//
// The reason is carried into the failure because the two ways of arriving here call for different
// fixes: no module means the install is incomplete, while no toolchain in a real checkout means the
// developer needs Go. A single message covering both would send half its readers the wrong way.
func resolveInstalled(reason error) (string, error) {
	// Ignored: SearchRoots passes over an empty start, so a working directory that can't be read leaves the
	// executable's own place to search, and a binary found in neither is the error below.
	workingDirectory, _ := os.Getwd()

	executablePath := ""
	if executable, executableError := os.Executable(); executableError == nil {
		// Symlinks are resolved because `node_modules/.bin/cohere` is one, and the package holding
		// the platform binary sits beside the real file rather than beside the link.
		if resolved, resolveSymlinksError := filepath.EvalSymlinks(executable); resolveSymlinksError == nil {
			executable = resolved
		}
		executablePath = executable
	}

	binaryPath, resolveError := release.Resolve(release.SearchRoots(workingDirectory, executablePath))
	if resolveError != nil {
		return "", fmt.Errorf("%w\n(no local build was possible: %w)", resolveError, reason)
	}
	return binaryPath, nil
}
