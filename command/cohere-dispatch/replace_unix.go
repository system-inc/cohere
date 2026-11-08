//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/system-inc/cohere/internal/streams"
)

// replaceProcess replaces this process with the cohere binary, so the binary inherits the terminal
// directly and its exit code is the one the caller sees. It returns only when the exec fails. See execute.
func replaceProcess(binaryPath string, arguments []string) (int, error) {
	// The exec drops this process's buffers whole, so what it said is delivered first, and output that never
	// arrived ends the run with its code rather than with the binary's.
	if exitCode := streams.Finish(0); exitCode != 0 {
		os.Exit(exitCode)
	}
	if execError := syscall.Exec(
		binaryPath,
		append([]string{binaryPath}, arguments...),
		os.Environ(),
	); execError != nil {
		return 0, fmt.Errorf("running %s: %w", binaryPath, execError)
	}
	return 0, nil
}
