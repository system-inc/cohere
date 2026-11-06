// Package lookalike holds shapes that read like a value built into a shell's script and are not, or that the rule
// cannot know and so leaves alone. Nothing here is ever run.
package lookalike

import (
	formatting "fmt"
	osexec "os/exec"
	"time"

	"lookalike/exec"
	"lookalike/fmt"
)

// A Command or Sprintf with os/exec's or fmt's spelling from another package is not theirs.
func otherFunctions(directory string) {
	_ = exec.Command("sh", "-c", "ls "+directory)
	_ = Command("sh", "-c", "ls "+directory)
	_ = osexec.Command("sh", "-c", fmt.Sprintf("ls %s", directory))
	_ = osexec.Command("sh", "-c", Sprintf("ls %s", directory))
}

// Command is this package's own, with the same name and shape as os/exec's.
func Command(name string, arguments ...string) string {
	return name
}

// Sprintf is this package's own, with the same name and shape as fmt's.
func Sprintf(format string, arguments ...any) string {
	return format
}

// Programs that are not POSIX shells have their own grammar for a script, or take no script at all.
func notAShell(directory string) {
	_ = osexec.Command("/usr/bin/env", "-c", "ls "+directory)
	_ = osexec.Command("python3", "-c", formatting.Sprintf("print(%q)", directory))
	_ = osexec.Command("osascript", "-e", "tell app \""+directory+"\" to quit")
	_ = osexec.Command("shell", "-c", "ls "+directory)
	_ = osexec.Command("fish", "-c", "ls "+directory)
}

// Without -c the shell runs a script file, and the arguments after the file are the script's, never parsed as code.
func shellWithoutInlineScript(name string, directory string) {
	_ = osexec.Command("sh", formatting.Sprintf("%s.sh", name))
	_ = osexec.Command("bash", "stop.sh", "-c", "ls "+directory)
	_ = osexec.Command("bash", "+c", "ls "+directory)
	_ = osexec.Command("sh", "-e", "build.sh", directory)
}

// A program, an option or a script only known at run time is not judged, since the rule would have to guess at it.
func unknownAtRunTime(program string, option string, script string, arguments []string, directory string) {
	_ = osexec.Command(program, "-c", "ls "+directory)
	_ = osexec.Command("sh", option, "-c", "ls "+directory)
	_ = osexec.Command("sh", arguments...)
	_ = osexec.Command("sh", "-c", script)
	_ = osexec.Command("sh", "-c", scriptOf(directory))
	_ = osexec.Command("bash", "--unknown-option", "-c", "ls "+directory)
	_ = osexec.Command("sh", "-c", "-x "+directory)
	_ = osexec.Command("sh", []string{0: "-c", 1: "ls " + directory}...)
	_ = &osexec.Cmd{Path: "/bin/sh", Args: []string{"sh", "-c", "ls " + directory}}
}

func scriptOf(directory string) string {
	return "ls " + directory
}

// listing is a package-level variable, which any file of the package may assign, so it is never followed.
var listing = "ls " + formatting.Sprint(time.Now())

// A local assigned more than once, or whose address is taken, may hold something other than its initializer when the
// shell runs it. Each one here starts built from a value and holds a constant by the time the shell runs it.
func localsAssignedAgain(directory string) (string, error) {
	_ = osexec.Command("sh", "-c", listing)

	overwritten := "ls " + directory
	overwritten = "ls"
	_ = osexec.Command("sh", "-c", overwritten)

	pointed := "ls " + directory
	reset(&pointed)
	_ = osexec.Command("sh", "-c", pointed)

	closured := "ls " + directory
	func() { closured = "ls" }()
	_ = osexec.Command("sh", "-c", closured)

	ranged := "ls " + directory
	for _, ranged = range []string{"ls"} {
	}
	_ = osexec.Command("sh", "-c", ranged)

	redeclared := "ls " + directory
	redeclared, readError := "ls", error(nil)
	_ = osexec.Command("sh", "-c", redeclared)
	return redeclared, readError
}

func reset(target *string) {
	*target = "ls"
}

// Level prints through its own String method, whose text the rule does not read.
type Level int

func (level Level) String() string {
	return "debug"
}

// A value whose text comes from a method, a type parameter, or arguments spread into Sprintf is not judged, since
// its text cannot be seen here.
func valuesNotJudged[Value any](wait time.Duration, level Level, value Value, arguments []any) {
	_ = osexec.Command("sh", "-c", formatting.Sprintf("sleep %s", wait))
	_ = osexec.Command("sh", "-c", formatting.Sprintf("run --level=%s", level))
	_ = osexec.Command("sh", "-c", formatting.Sprintf("echo %v", value))
	_ = osexec.Command("sh", "-c", formatting.Sprintf("kill %d %d", arguments...))
}

// A << in the written text may open a heredoc, so the rule declines the script: an unquoted heredoc and a here-string
// are missed with the quoted heredoc it means to leave alone.
func heredocsDeclined(value string) {
	_ = osexec.Command("sh", "-c", "cat <<EOF\n"+value+"\nEOF")
	_ = osexec.Command("bash", "-c", "grep x <<< "+value)
}
