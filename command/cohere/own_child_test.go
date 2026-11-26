package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// A test that needs a child process starts this test binary as one, playing a role, rather than a program of
// the machine's: the tests start no process but their own (Kirk's ruling, Nov 25). TestMain reads
// ownChildVariable before anything else and, when it is set, plays the role and exits, so no test runs in a
// child.

// ownChildVariable names the role a child started by ownChildCommand plays.
const ownChildVariable = "COHERE_TEST_OWN_CHILD"

// ownChildRole is what a child of this test binary does in place of running the tests.
//
//   - Exit exits with the code its one argument names.
//   - Sleep waits until the lifeline its one argument names is gone, or ownChildLifetime has passed.
//   - Descriptor prints open or closed, for the descriptor its one argument numbers.
//   - StartHolder starts a Sleep child holding its own standard output and error, writes that child's id to its
//     second argument, and exits without waiting, so whoever reads its output waits on the Sleep. Its first argument
//     is the Sleep's lifeline.
type ownChildRole string

const (
	ownChildExit        ownChildRole = "Exit"
	ownChildSleep       ownChildRole = "Sleep"
	ownChildDescriptor  ownChildRole = "Descriptor"
	ownChildStartHolder ownChildRole = "StartHolder"
)

// ownChildLifetime is the longest a Sleep child lives with its lifeline still there: past go test's default
// -timeout of ten minutes, so a test that waits on one hangs until -timeout fails it, and short of a day, so one
// a crashed test binary left behind still ends by itself.
const ownChildLifetime = 15 * time.Minute

// ownChildPollInterval is how often a Sleep child looks for its lifeline.
const ownChildPollInterval = 50 * time.Millisecond

// ownChildMisused is the exit code of a child started without the arguments its role needs, apart from any code
// the Exit role is asked for in this package's tests.
const ownChildMisused = 125

// ownChildCommand is this test binary, started as a child playing role.
func ownChildCommand(role ownChildRole, arguments ...string) (*exec.Cmd, error) {
	executable, executableError := os.Executable()
	if executableError != nil {
		return nil, fmt.Errorf("finding this test binary: %w", executableError)
	}
	command := exec.Command(executable, arguments...)
	// Later entries win in exec, so this names the role even in a child of a child that played another.
	command.Env = append(os.Environ(), ownChildVariable+"="+string(role))
	return command, nil
}

// ownChild is ownChildCommand for a test, and the child it starts ends with the test: its cleanup kills the child
// by the id this command's own start returned. A child already waited for is not signalled, since Go knows it has
// ended.
func ownChild(test testing.TB, role ownChildRole, arguments ...string) *exec.Cmd {
	test.Helper()
	command, commandError := ownChildCommand(role, arguments...)
	if commandError != nil {
		test.Fatal(commandError)
	}
	test.Cleanup(func() {
		if command.Process != nil {
			// Ignored: a child that ended, or was waited for, is the outcome the kill asks for.
			_ = command.Process.Kill()
		}
	})
	return command
}

// ownChildLifeline is a file whose removal ends every Sleep child handed it, removed when the test ends. A child a
// test did not start itself, one a stand-in script started, is ended this way, since the test holds no command of
// its own to kill it by.
func ownChildLifeline(test testing.TB) string {
	test.Helper()
	lifeline := filepath.Join(test.TempDir(), "lifeline")
	if writeError := os.WriteFile(lifeline, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	test.Cleanup(func() {
		// Ignored: the directory's own cleanup removes it again, and a Sleep child reads a lifeline it cannot find as
		// gone either way.
		_ = os.Remove(lifeline)
	})
	return lifeline
}

// playOwnChild plays role with arguments and returns the code to exit with.
func playOwnChild(role ownChildRole, arguments []string) int {
	misused := func() int {
		// Ignored: the exit code says the child was misused whether or not this line is read.
		_, _ = fmt.Fprintf(os.Stderr, "own child %s: unexpected arguments %q\n", role, arguments)
		return ownChildMisused
	}
	switch role {
	case ownChildExit:
		if len(arguments) != 1 {
			return misused()
		}
		code, parseError := strconv.Atoi(arguments[0])
		if parseError != nil {
			return misused()
		}
		return code
	case ownChildSleep:
		if len(arguments) != 1 {
			return misused()
		}
		deadline := time.Now().Add(ownChildLifetime)
		for ; time.Now().Before(deadline); time.Sleep(ownChildPollInterval) {
			if _, statError := os.Stat(arguments[0]); statError != nil {
				return 0
			}
		}
		return 0
	case ownChildDescriptor:
		if len(arguments) != 1 {
			return misused()
		}
		state := "open"
		if _, statError := os.Stat("/dev/fd/" + arguments[0]); statError != nil {
			state = "closed"
		}
		// Ignored: the test reads this line, and a write that failed fails the test on the line it does not find.
		_, _ = fmt.Println(state)
		return 0
	case ownChildStartHolder:
		if len(arguments) != 2 {
			return misused()
		}
		holder, commandError := ownChildCommand(ownChildSleep, arguments[0])
		if commandError != nil {
			return misused()
		}
		holder.Stdout, holder.Stderr = os.Stdout, os.Stderr
		if startError := holder.Start(); startError != nil {
			return misused()
		}
		writeError := os.WriteFile(arguments[1], []byte(strconv.Itoa(holder.Process.Pid)), 0o644)
		if writeError != nil {
			return misused()
		}
		return 0
	}
	return misused()
}
