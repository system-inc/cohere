// Package allowed stops processes by pid, which the rule leaves alone. Nothing here is ever run.
package allowed

import (
	"os/exec"
	"strconv"
	"syscall"
)

func stopByPid(command *exec.Cmd, pid int) {
	_ = command.Process.Kill()
	_ = syscall.Kill(pid, syscall.SIGTERM)
	_ = exec.Command("kill", "1234")
	_ = exec.Command("kill", "-9", "1234")
	_ = exec.Command("kill", "-TERM", strconv.Itoa(pid))
	_ = exec.Command("kill", "-SIGKILL", "1234", "5678")
	_ = exec.Command("kill", "-s", "TERM", "1234")
	_ = exec.Command("kill", "--signal", "TERM", "1234")
	_ = exec.Command("kill", "--", "-1234")
	_ = exec.Command("kill", "--timeout", "1000", "KILL", "1234")
	_ = exec.Command("kill", []string{"-9", strconv.Itoa(pid)}...)
	_ = exec.Command("sh", "-c", "kill 1234")
}

// Listing signals stops nothing, and the operand of -l is a signal, not a process.
func listSignals() {
	_ = exec.Command("kill", "-l")
	_ = exec.Command("kill", "-l", "TERM")
}

// Programs that find or report processes by name stop none.
func findByName() {
	_ = exec.Command("pgrep", "worker")
	_ = exec.Command("sh", "-c", "pgrep -f worker")
	_ = exec.Command("ps", "-A")
}
