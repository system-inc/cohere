//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup makes the engine lead a process group of its own, so ending it can end
// everything it started.
func startInOwnProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessGroup sends a signal to every process in the group the engine leads. With Setpgid the
// engine leads its group, so the group's id is the engine's process id.
func signalProcessGroup(engineGroup int, signal os.Signal) {
	if forwarded, isSignal := signal.(syscall.Signal); isSignal {
		// Ignored: a group that already ended has nothing left to pass the signal to, and the engine's own exit is
		// what the run reports.
		_ = syscall.Kill(-engineGroup, forwarded)
	}
}

// killProcessGroup kills every process in the group the engine leads.
func killProcessGroup(engineGroup int) {
	// Ignored: a group already gone is the outcome asked for, and killEngineGroup kills again until the engine is
	// reaped.
	_ = syscall.Kill(-engineGroup, syscall.SIGKILL)
}
