// Package dotted calls exec.Command through a dot import and a renamed import.
package dotted

import (
	. "os/exec"
	run "os/exec"
)

func dotted() {
	Command("ls -l")
	run.Command("ls -l")
	run.Command("ls")
}
