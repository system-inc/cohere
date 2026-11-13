//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup is the process attributes that start a command as the leader of a process group of its own, so
// stopGroup reaches everything it starts: the engine runs go list and cgo, which a stop aimed at its pid alone would
// leave running past the deadline.
func ownProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// stopGroup kills the process group a started command leads, which ownProcessGroup gave it, children and all.
func stopGroup(command *exec.Cmd) error {
	return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
}
