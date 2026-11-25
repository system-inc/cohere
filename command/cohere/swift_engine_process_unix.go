//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// startInOwnProcessGroup makes the child lead a process group of its own, so ending it can end
// everything it started.
//
// Mutates: command the caller's command is the one started, so the group is set on it before it starts
func startInOwnProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessGroup sends a signal to every process in the group the child leads. With Setpgid the
// child leads its group, so the group's id is the child's process id.
func signalProcessGroup(childGroup int, signal os.Signal) {
	if forwarded, isSignal := signal.(syscall.Signal); isSignal {
		// Ignored: a group that already ended has nothing left to pass the signal to, and the child's own exit is
		// what the run reports.
		_ = syscall.Kill(-childGroup, forwarded)
	}
}

// killProcessGroup kills every process in the group the child leads.
func killProcessGroup(childGroup int) {
	// Ignored: a group already gone is the outcome asked for, and killContractGroup kills again until the child is
	// reaped.
	_ = syscall.Kill(-childGroup, syscall.SIGKILL)
}
