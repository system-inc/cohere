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
	"syscall"

	"github.com/system-inc/cohere/internal/release/dispatch"
	"github.com/system-inc/cohere/internal/release/packaging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cohere: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// The dispatcher owns exactly three flags and forwards everything else. They are matched
	// positionally at the front rather than with the flag package, because flag.Parse would stop at
	// the first argument it does not recognize and swallow flags meant for the real binary.
	arguments := os.Args[1:]
	development := false
	verbose := false
	frozen := false

	for len(arguments) > 0 {
		switch arguments[0] {
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
			goto parsed
		}
	}
parsed:

	if frozen {
		binaryPath, err := resolveFrozenBinary()
		if err != nil {
			return err
		}
		return execute(binaryPath, arguments)
	}

	binaryPath, err := resolveBinary(development, verbose)
	if err != nil {
		return err
	}

	return execute(binaryPath, arguments)
}

// execute replaces this process with the cohere binary.
//
// exec rather than spawn-and-wait: the real binary takes over, so it inherits the terminal directly
// and its exit code is the one the caller sees. A wrapper that forwarded the status would be one
// more layer able to lose a non-zero exit, and losing a non-zero exit is how a gate goes quietly
// green.
func execute(binaryPath string, arguments []string) error {
	if err := syscall.Exec(binaryPath, append([]string{binaryPath}, arguments...), os.Environ()); err != nil {
		return fmt.Errorf("running %s: %w", binaryPath, err)
	}
	return nil
}

// resolveFrozenBinary picks the newest cached binary and announces that it did.
//
// The announcement is the feature, not decoration. Freezing runs a binary whose inputs were never
// compared against the rules on disk, so the one thing that separates it from the stale-binary
// defect is that the operator is told: which binary, when it was built, and that it may not match.
// A stated limitation is not a lie; silence would be. This prints to stderr so it survives a caller
// piping stdout, and it prints before the exec because after the exec there is no "after".
func resolveFrozenBinary() (string, error) {
	moduleDirectory, err := dispatch.FindModuleDirectory()
	if err != nil {
		return "", err
	}

	frozen, err := dispatch.ResolveFrozen(dispatch.DefaultPaths(moduleDirectory))
	if err != nil {
		return "", err
	}

	fmt.Fprintf(os.Stderr, "cohere: --frozen, so the rules on disk were never checked against this binary.\n")
	fmt.Fprintf(os.Stderr, "  running: %s\n", filepath.Base(frozen.Path))
	fmt.Fprintf(os.Stderr, "  hash:    %s\n", frozen.Hash)
	fmt.Fprintf(os.Stderr, "  built:   %s\n", frozen.ModifiedAt)

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

	moduleDirectory, moduleErr := dispatch.FindModuleDirectory()

	// No module means an installed package: there is no source to build from, so the shipped
	// binary is the only option, and a missing one is an error naming the platform.
	if moduleErr != nil {
		return resolveInstalled(moduleErr)
	}

	if !dispatch.HasToolchain() {
		// Source is present but nothing can compile it. The shipped binary is still the right
		// answer, and it is still never a fallback to something that might do nothing.
		return resolveInstalled(dispatch.ErrNoToolchain)
	}

	paths := dispatch.DefaultPaths(moduleDirectory)

	if development {
		binaryPath, built, err := dispatch.ResolveWorkingTree(paths, "./command/cohere")
		if err != nil {
			return "", err
		}
		// On every run, not only when it rebuilds. The binary carries every member's uncommitted edits
		// in the shared checkout, and a reader of its findings has to know that before they read them.
		fmt.Fprintf(os.Stderr, "cohere: --dev runs the working tree in %s, uncommitted edits included "+
			"(anyone's, not only yours), so no commit reproduces these results\n", moduleDirectory)
		if built && verbose {
			fmt.Fprintf(os.Stderr, "cohere: working tree changed, rebuilt %s\n", filepath.Base(binaryPath))
		}
		return binaryPath, nil
	}

	binaryPath, commit, built, err := dispatch.ResolveCommitted(paths, "./command/cohere")
	if err != nil {
		return "", err
	}
	if built && verbose {
		fmt.Fprintf(os.Stderr, "cohere: built %s from commit %s\n", filepath.Base(binaryPath), commit)
	}
	return binaryPath, nil
}

// resolveInstalled finds the shipped platform binary, reporting why building was not an option.
//
// The reason is carried into the failure because the two ways of arriving here call for different
// fixes: no module means the install is incomplete, while no toolchain in a real checkout means the
// developer needs Go. A single message covering both would send half its readers the wrong way.
func resolveInstalled(reason error) (string, error) {
	workingDirectory, _ := os.Getwd()

	executablePath := ""
	if executable, err := os.Executable(); err == nil {
		// Symlinks are resolved because `node_modules/.bin/cohere` is one, and the package holding
		// the platform binary sits beside the real file rather than beside the link.
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		executablePath = executable
	}

	binaryPath, err := release.Resolve(release.SearchRoots(workingDirectory, executablePath))
	if err != nil {
		return "", fmt.Errorf("%w\n(no local build was possible: %s)", err, reason)
	}
	return binaryPath, nil
}
