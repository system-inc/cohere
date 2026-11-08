//go:build !unix

package dispatch

import (
	"os/exec"
	"syscall"
)

// startInOwnGroup does nothing where there are no Unix process groups. The tool it would group is the
// Swift engine's build, which runs only on macOS.
func startInOwnGroup(*exec.Cmd) {
	// There are no process groups here, so the tool starts as any child does.
}

// signalGroup kills the tool itself, the one process this platform lets it end.
func signalGroup(command *exec.Cmd, _ syscall.Signal) {
	// Ignored: a tool that has already ended has nothing left to kill, and the caller waits for its end either way.
	_ = command.Process.Kill()
}
