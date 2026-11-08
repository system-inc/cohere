package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"

	"github.com/system-inc/cohere/internal/streams"
)

// replaceProcess runs the cohere binary and returns its exit code for this process to exit with, the nearest
// Windows has to exec: it has no call that replaces a process, and syscall.Exec there fails every time.
//
// The binary inherits the console's three handles, so it reads and writes the terminal itself. Ctrl+C
// reaches every process on the console, the binary included, so this one ignores it and waits for the
// binary to end however it chooses to. Its exit code is passed on as it is, since a wrapper that lost a
// non-zero exit is how a gate goes quietly green.
func replaceProcess(binaryPath string, arguments []string) (int, error) {
	command := exec.Command(binaryPath, arguments...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	// The binary writes to the console itself, so what this process said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	signal.Ignore(os.Interrupt)
	runError := command.Run()
	var exitError *exec.ExitError
	if runError != nil && !errors.As(runError, &exitError) {
		return 0, fmt.Errorf("running %s: %w", binaryPath, runError)
	}
	return command.ProcessState.ExitCode(), nil
}
