//go:build unix

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// errorRecorder is a testing.TB that keeps what Errorf was handed instead of failing.
type errorRecorder struct {
	testing.TB
	messages []string
}

func (recorder *errorRecorder) Helper() {
	// The recorder reports no file and line of its own, so there is no frame to mark as a helper.
}

func (recorder *errorRecorder) Errorf(format string, arguments ...any) {
	recorder.messages = append(recorder.messages, fmt.Sprintf(format, arguments...))
}

// exitErrorOf is the exit error a child's run or wait ended with, failing the test when it ended with none.
func exitErrorOf(test *testing.T, runError error) *exec.ExitError {
	test.Helper()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) {
		test.Fatalf("the child ended with %v, want an exit error", runError)
	}
	return exitError
}

// A child a signal ended is named as killed, by its signal, and a child that chose its exit is not. Both children
// are this test binary: one sleeping until this test kills it by its own command's process, one exiting 3.
func TestChildExitCodeNamesTheSignalThatEndedAChild(test *testing.T) {
	test.Parallel()

	sleeper := ownChild(test, ownChildSleep, ownChildLifeline(test))
	if startError := sleeper.Start(); startError != nil {
		test.Fatal(startError)
	}
	if killError := sleeper.Process.Kill(); killError != nil {
		test.Fatal(killError)
	}
	killed := &errorRecorder{TB: test}
	if code := childExitCode(killed, exitErrorOf(test, sleeper.Wait())); code != -1 {
		test.Errorf("a killed child read as exit %d, want -1", code)
	}
	if len(killed.messages) != 1 || !strings.Contains(killed.messages[0], "killed by signal 9 (killed)") {
		test.Errorf("a killed child was reported as %q", killed.messages)
	}

	chose := &errorRecorder{TB: test}
	if code := childExitCode(chose, exitErrorOf(test, ownChild(test, ownChildExit, "3").Run())); code != 3 ||
		len(chose.messages) != 0 {
		test.Errorf("a child that exited 3 read as %d with %q", code, chose.messages)
	}
}
