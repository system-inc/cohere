//go:build windows

package main

import (
	"os"
	"os/exec"
)

// Windows has no process groups to signal as one, and cohere-swift is built and run on macOS, so
// these end the child process itself. Its children are not reached here, which is the behavior every
// platform had before the child got a group of its own.

func startInOwnProcessGroup(*exec.Cmd) {
	// Windows has no process groups, so the child starts as any child does.
}

func signalProcessGroup(childGroup int, _ os.Signal) {
	killProcessGroup(childGroup)
}

func killProcessGroup(childGroup int) {
	if process, findProcessError := os.FindProcess(childGroup); findProcessError == nil {
		// Ignored: a child already gone is the outcome asked for, and killContractGroup kills again until the child
		// is reaped.
		_ = process.Kill()
	}
}
