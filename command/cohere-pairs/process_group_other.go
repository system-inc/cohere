//go:build !unix

package main

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup is no attributes off unix, which has no process group to start a command in. A pair is measured on
// linux and darwin; this is here so the module builds everywhere it is built.
func ownProcessGroup() *syscall.SysProcAttr {
	return nil
}

// stopGroup kills the started command alone off unix, where ownProcessGroup made no group.
func stopGroup(command *exec.Cmd) error {
	return command.Process.Kill()
}
