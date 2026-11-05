// Package flagged holds every way of building a value into a shell's script the rule must report. Nothing here is
// ever run.
package flagged

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
)

// Branch is a named string type, which holds whatever text it was converted from.
type Branch string

// shell is a named constant, which the type checker knows as well as a literal.
const shell = "bash"

func sprintfAndConcatenation(requestContext context.Context, directory string, url string) {
	_ = exec.Command("sh", "-c", fmt.Sprintf("cd %s && make", directory))     // want "run by a shell"
	_ = exec.Command("sh", "-c", "open "+url)                                 // want "run by a shell"
	_ = exec.Command("sh", "-c", "open '"+url+"'")                            // want "run by a shell"
	_ = exec.CommandContext(requestContext, "bash", "-c", "ls "+directory)    // want "run by a shell"
	_ = exec.Command(shell, "-c", "ls "+directory)                            // want "run by a shell"
	_ = exec.Command("/bin/zsh", "-c", "ls "+directory)                       // want "run by a shell"
	_ = exec.Command("dash", "-c", ("ls " + directory))                       // want "run by a shell"
	_ = exec.Command("sh", "-c", "echo é "+directory)                         // want "run by a shell"
	_ = exec.Command("sh", "-c", "ls "+fmt.Sprintf("%s/build", directory))    // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("ls %s", "/tmp/"+directory))     // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("ls %s", directory), "sh", "x")  // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("ls %d %s", 3, directory))       // want "run by a shell"
	_ = exec.Command("sh", "-c", "kill "+strconv.Itoa(4)+" && rm "+directory) // want "run by a shell"
}

// The options before the script are read as the shell reads them, so the script is found after any of them.
func optionsBeforeTheScript(directory string) {
	_ = exec.Command("bash", "-lc", "ls "+directory)                            // want "run by a shell"
	_ = exec.Command("bash", "-ec", "ls "+directory)                            // want "run by a shell"
	_ = exec.Command("bash", "-c", "-e", "ls "+directory)                       // want "run by a shell"
	_ = exec.Command("bash", "-o", "pipefail", "-c", "ls "+directory)           // want "run by a shell"
	_ = exec.Command("bash", "-eo", "pipefail", "-c", "ls "+directory)          // want "run by a shell"
	_ = exec.Command("bash", "--login", "-c", "ls "+directory)                  // want "run by a shell"
	_ = exec.Command("bash", "--rcfile", "profile", "-c", "ls "+directory)      // want "run by a shell"
	_ = exec.Command("bash", "+x", "-c", "ls "+directory)                       // want "run by a shell"
	_ = exec.Command("sh", "-c", "--", "ls "+directory)                         // want "run by a shell"
	_ = exec.Command("sh", []string{"-c", "ls " + directory}...)                // want "run by a shell"
	_ = exec.Command("ksh", []string{"-c", fmt.Sprintf("ls %s", directory)}...) // want "run by a shell"
}

// %q adds double quotes and backslash escapes, and inside double quotes the shell still runs $(...) and backticks.
func quotedVerbs(branch string) {
	_ = exec.Command("sh", "-c", fmt.Sprintf("git log %q", branch))   // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("git log '%s'", branch)) // want "run by a shell"
}

// A value of any type that prints its contents is text the shell parses.
func valuesThatPrintText(branch Branch, failure error, value any, names []string, format string) {
	_ = exec.Command("sh", "-c", fmt.Sprintf("git log %s", branch))     // want "run by a shell"
	_ = exec.Command("sh", "-c", "git log "+string(branch))             // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("echo %v", failure))       // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("echo %v", value))         // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("rm %v", names))           // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf(format, 3))                // want "run by a shell"
	_ = exec.Command("sh", "-c", fmt.Sprintf("echo %d", directoryOf())) // want "run by a shell"
}

func directoryOf() string {
	return "/tmp"
}

// A local assigned once holds only its initializer, so a script built there is built here.
func builtInALocal(directory string) {
	script := fmt.Sprintf("cd %s && make", directory)
	_ = exec.Command("sh", "-c", script) // want "run by a shell"

	var listing = "ls " + directory
	_ = exec.Command("sh", "-c", listing) // want "run by a shell"

	counted := listing + " | wc -l"
	_ = exec.Command("sh", "-c", counted) // want "run by a shell"
}
