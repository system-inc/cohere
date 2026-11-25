// Package differences holds the shapes where the port's range could part from the original's: a program on a line
// of its own, beside comments, inside parentheses, joined from constants over lines, and a call spread over lines.
// The port and the original find the same here, so the fixture records no difference.
package differences

import "os/exec"

const flags = " -l"

func shapes() {
	exec.Command(
		"ls -l",
	)
	exec.Command( /* before */ "ls -l" /* after */)
	exec.Command("ls -l") // trailing
	exec.Command(("ls -l"))
	exec.Command("ls" +
		flags)
	exec.Command(
		// leading
		"ls -l",
		"-a",
	)
}
