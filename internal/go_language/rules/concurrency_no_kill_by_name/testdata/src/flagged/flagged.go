// Package flagged holds every way of stopping processes by name the rule must report. Nothing here is ever run.
package flagged

import (
	"context"
	"os/exec"
)

// stopper is a named constant, which the type checker knows as well as a literal.
const stopper = "killall"

func byNamePrograms(requestContext context.Context) {
	_ = exec.Command("pkill", "-f", "worker")                  // want "by name through .pkill."
	_ = exec.Command("killall", "worker")                      // want "by name through .killall."
	_ = exec.CommandContext(requestContext, "pkill", "worker") // want "by name through .pkill."
	_ = exec.Command(stopper, "worker")                        // want "by name through .killall."
	_ = exec.Command("/usr/bin/pkill", "worker")               // want "by name through .pkill."
	_ = exec.Command(("pkill"), "worker")                      // want "by name through .pkill."
}

// A slice built elsewhere hides the arguments, and pkill stops by name whatever they are.
func byNameWithUnknownArguments(arguments []string) {
	_ = exec.Command("pkill", arguments...) // want "by name through .pkill."
}

func killGivenAName(requestContext context.Context) {
	_ = exec.Command("kill", "worker")                              // want "by name through .kill."
	_ = exec.Command("kill", "-9", "worker")                        // want "by name through .kill."
	_ = exec.Command("kill", "-TERM", "worker")                     // want "by name through .kill."
	_ = exec.Command("kill", "-s", "TERM", "worker")                // want "by name through .kill."
	_ = exec.Command("kill", "1234", "worker")                      // want "by name through .kill."
	_ = exec.Command("kill", "--", "worker")                        // want "by name through .kill."
	_ = exec.Command("kill", []string{"-TERM", "worker"}...)        // want "by name through .kill."
	_ = exec.CommandContext(requestContext, "/bin/kill", "worker")  // want "by name through .kill."
	_ = exec.Command("kill", "--timeout", "1000", "KILL", "worker") // want "by name through .kill."
}

func shellScripts() {
	_ = exec.Command("sh", "-c", "pkill -f worker")                         // want "by name through .pkill."
	_ = exec.Command("bash", "-c", "make stop; killall worker")             // want "by name through .killall."
	_ = exec.Command("zsh", "-lc", "/usr/bin/pkill worker")                 // want "by name through .pkill."
	_ = exec.Command("bash", "-ec", "pkill\tworker")                        // want "by name through .pkill."
	_ = exec.Command("bash", "-o", "pipefail", "-c", "pkill worker")        // want "by name through .pkill."
	_ = exec.Command("/bin/sh", []string{"-c", "killall worker"}...)        // want "by name through .killall."
	_ = exec.Command("sh", "-c", "echo stopping && (pkill worker || true)") // want "by name through .pkill."
}
