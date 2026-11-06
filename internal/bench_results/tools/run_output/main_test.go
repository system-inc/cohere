package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unwritableChildVariable tells a test binary run by TestAColumnThatNeverArrivesExitsOne to be the run_output
// whose standard output takes no writes.
const unwritableChildVariable = "RUNOUTPUT_UNWRITABLE_CHILD"

// A run whose columns cannot be written exits 1, so quiet_machine.sh stops rather than recording a column it
// could not fill. The test binary runs itself as a run_output reading a real run's log, with standard output
// swapped for a file opened only for reading, so the columns' write fails. The child is a process of its own
// running this test alone, so the globals it sets are shared with no other test, and the test runs in parallel.
func TestAColumnThatNeverArrivesExitsOne(test *testing.T) {
	test.Parallel()
	runLog := filepath.Join("..", "..", "testdata", "run-output", "cold.txt")
	if os.Getenv(unwritableChildVariable) == "1" {
		readOnly, openError := os.Open(os.DevNull)
		if openError != nil {
			test.Fatal(openError)
		}
		os.Stdout = readOnly
		os.Args = []string{"run_output", runLog}
		main()
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run=^TestAColumnThatNeverArrivesExitsOne$")
	command.Env = append(os.Environ(), unwritableChildVariable+"=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runError := command.Run()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) || exitError.ExitCode() != 1 {
		test.Fatalf(
			"the run_output that could not print its columns ended with %v, want exit 1:\n%s",
			runError,
			stderr.String(),
		)
	}
	if !strings.Contains(stderr.String(), "run_output: printing the columns: ") {
		test.Fatalf("the run_output that could not print its columns did not say so:\n%s", stderr.String())
	}
}
