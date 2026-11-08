//go:build windows

package main

import (
	"os"
	"os/exec"
)

// Windows has no process groups to signal as one, and the Swift engine is built and run on macOS, so
// these end the engine process itself. Its children are not reached here, which is the behavior every
// platform had before the engine got a group of its own.

func startInOwnProcessGroup(*exec.Cmd) {
	// Windows has no process groups, so the engine starts as any child does.
}

func signalProcessGroup(engineGroup int, _ os.Signal) {
	killProcessGroup(engineGroup)
}

func killProcessGroup(engineGroup int) {
	if process, findProcessError := os.FindProcess(engineGroup); findProcessError == nil {
		// Ignored: an engine already gone is the outcome asked for, and killEngineGroup kills again until the engine
		// is reaped.
		_ = process.Kill()
	}
}
