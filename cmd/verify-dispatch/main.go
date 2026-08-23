// Command verify-dispatch is the shim that `node_modules/.bin/verify` runs.
//
// It decides which verify binary should handle this invocation and execs it, so that the common
// case costs one stat and one exec. When the rules have changed it rebuilds first, which is the
// price of compiling rules in rather than loading them, paid automatically instead of by hand.
//
// Every flag it does not own is passed through untouched, because it stands in front of the real
// command rather than wrapping it.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/system-inc/verify/internal/dispatch"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "verify: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// The dispatcher owns exactly two flags and forwards everything else. They are matched
	// positionally at the front rather than with the flag package, because flag.Parse would stop at
	// the first argument it does not recognize and swallow flags meant for the real binary.
	arguments := os.Args[1:]
	development := false
	verbose := false

	for len(arguments) > 0 {
		switch arguments[0] {
		case "--dev":
			development = true
			arguments = arguments[1:]
		case "--dispatch-verbose":
			verbose = true
			arguments = arguments[1:]
		default:
			goto parsed
		}
	}
parsed:

	moduleDirectory, err := findModuleDirectory()
	if err != nil {
		return err
	}

	binaryPath, err := resolveBinary(moduleDirectory, development, verbose)
	if err != nil {
		return err
	}

	// exec rather than spawn-and-wait: the real binary replaces this process, so it inherits the
	// terminal directly and its exit code is the one the caller sees. A wrapper that forwarded the
	// status would be one more layer able to lose a non-zero exit, and losing a non-zero exit is
	// how a gate goes quietly green.
	arguments = append([]string{binaryPath}, arguments...)
	if err := syscall.Exec(binaryPath, arguments, os.Environ()); err != nil {
		return fmt.Errorf("running %s: %w", binaryPath, err)
	}
	return nil
}

// resolveBinary picks the binary to run: a locally built one when there is a toolchain, the shipped
// one when there is not.
func resolveBinary(moduleDirectory string, development bool, verbose bool) (string, error) {
	if !dispatch.HasToolchain() {
		// No toolchain means no rebuild is possible, so the shipped binary is the only option. If
		// there is none for this platform, that is a loud error naming the platform. It is never a
		// fallback to something that might do nothing.
		return dispatch.ResolvePrebuilt(moduleDirectory)
	}

	paths := dispatch.DefaultPaths(moduleDirectory)

	binaryPath, built, err := dispatch.Resolve(paths, "./cmd/verify", development)
	if err != nil {
		return "", err
	}

	if built && verbose {
		fmt.Fprintf(os.Stderr, "verify: rules changed, rebuilt %s\n", filepath.Base(binaryPath))
	}
	return binaryPath, nil
}

// findModuleDirectory walks up from this executable and from the working directory looking for the
// verify module.
//
// It returns an error naming what it looked for rather than guessing. A resolver that gives up and
// returns a bare command name is exactly the bug this tool was built to stop shipping.
func findModuleDirectory() (string, error) {
	candidates := []string{}

	if executable, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		candidates = append(candidates, filepath.Dir(executable))
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		candidates = append(candidates, workingDirectory)
	}

	for _, candidate := range candidates {
		if directory, found := walkUpForModule(candidate); found {
			return directory, nil
		}
	}

	return "", errors.New("could not find the verify module: no go.mod declaring github.com/system-inc/verify in any parent of this binary or the working directory")
}

// walkUpForModule climbs toward the filesystem root looking for the verify module's go.mod.
func walkUpForModule(start string) (string, bool) {
	directory := start
	for {
		contents, err := os.ReadFile(filepath.Join(directory, "go.mod"))
		if err == nil && isVerifyModule(contents) {
			return directory, true
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return "", false
		}
		directory = parent
	}
}

// isVerifyModule reports whether a go.mod declares this module.
//
// The module path is matched rather than just the presence of a go.mod, so that a shim sitting
// inside some other Go project does not mistake that project for its own module.
func isVerifyModule(contents []byte) bool {
	for line := range strings.SplitSeq(string(contents), "\n") {
		if strings.TrimSpace(line) == "module github.com/system-inc/verify" {
			return true
		}
	}
	return false
}
