//go:build unix

package dispatch

import (
	"os/exec"
	"syscall"
)

// startInOwnGroup makes a build tool lead a process group of its own, so ending the group ends every
// compiler it started.
//
// Mutates: command its process attributes are set here, before the caller starts it, since a group is chosen at start
func startInOwnGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup sends a signal to every process in the group the tool leads. With Setpgid the tool leads
// its group, so the group's id is the tool's process id.
func signalGroup(command *exec.Cmd, signal syscall.Signal) {
	// Ignored: a group that has already ended has nothing left to signal, and the caller waits for the tool's end
	// either way.
	_ = syscall.Kill(-command.Process.Pid, signal)
}
