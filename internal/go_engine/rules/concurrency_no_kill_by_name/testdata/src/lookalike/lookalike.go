// Package lookalike holds shapes that read like a stop by name and are not, or that the rule cannot know. Nothing
// here is ever run.
package lookalike

import (
	osexec "os/exec"

	"lookalike/exec"
)

// A Command with os/exec's spelling from another package is not os/exec's.
func otherCommand() {
	_ = exec.Command("pkill", "worker")
	_ = exec.Command("kill", "worker")
	_ = Command("killall", "worker")
}

// Command is this package's own, with the same name and shape as os/exec's.
func Command(name string, arguments ...string) string {
	return name
}

// A killer's name that is part of a longer word, or not followed by arguments, is not a script running it.
func scriptsWithoutAKiller() {
	_ = osexec.Command("sh", "-c", "echo mypkill worker")
	_ = osexec.Command("sh", "-c", "run-pkill worker")
	_ = osexec.Command("bash", "-c", "echo pkilled")
	_ = osexec.Command("bash", "-c", "grep killallow notes.txt")
}

// Without -c the shell runs a script file, and the arguments after it are the script's, so a -c there is no option.
func shellWithoutInlineScript() {
	_ = osexec.Command("sh", "stop.sh", "pkill worker")
	_ = osexec.Command("bash", "stop.sh", "-c", "pkill worker")
}

// A killer's name as an argument of another program stops nothing.
func killerAsData() {
	_ = osexec.Command("echo", "pkill worker")
	_ = osexec.Command("man", "killall")
}

// A program or script only known at run time is not judged, since the rule would have to guess at it.
func unknownAtRunTime(program string, script string, operand string, arguments []string) {
	_ = osexec.Command(program, "worker")
	_ = osexec.Command("sh", "-c", script)
	_ = osexec.Command("kill", operand)
	_ = osexec.Command("kill", arguments...)
	_ = osexec.Command("sh", arguments...)
}
