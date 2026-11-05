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

// closedOutputChildVariable tells a test binary run by TestASectionThatCannotBePrintedExitsOne to be the
// command, with its standard output closed.
const closedOutputChildVariable = "CHANGELOG_CHILD_CLOSED_OUTPUT"

// The section on stdout is the command's whole product, a person pastes it, so a section that could not be
// printed ends the command with exit code 1 and the reason, where it once exited 0 having printed nothing. The
// test binary runs itself as the command, writing the first release's section into a closed stdout. The child is
// a process of its own running this test alone, so the arguments and stdout it sets are shared with no other test.
func TestASectionThatCannotBePrintedExitsOne(test *testing.T) {
	test.Parallel()
	if os.Getenv(closedOutputChildVariable) == "1" {
		module, moduleError := filepath.Abs(filepath.Join("..", "..", "..", ".."))
		if moduleError != nil {
			test.Fatal(moduleError)
		}
		if closeError := os.Stdout.Close(); closeError != nil {
			test.Fatal(closeError)
		}
		os.Args = []string{"changelog", "--version", "0.1.0", "--module", module, "--date", "2026-11-05"}
		main()
		return
	}
	executable, executableError := os.Executable()
	if executableError != nil {
		test.Fatal(executableError)
	}
	command := exec.Command(executable, "-test.run=^TestASectionThatCannotBePrintedExitsOne$")
	command.Env = append(os.Environ(), closedOutputChildVariable+"=1")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	runError := command.Run()
	var exitError *exec.ExitError
	if !errors.As(runError, &exitError) || exitError.ExitCode() != 1 {
		test.Fatalf("the command ended with %v, want exit code 1:\n%s", runError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "changelog: ") {
		test.Fatalf("the command did not say why it stopped:\n%s", stderr.String())
	}
}
