// Package lookalike holds shapes that look like go test -count=1 and are not. Nothing here runs.
package lookalike

import (
	"fmt"
	osexec "os/exec"

	"lookalike/exec"
)

// A function named Command in a package named exec that is not os/exec.
func notOsExec() {
	exec.Command("go", "test", "-count=1")
	exec.CommandContext(nil, "go", "test", "-count=1")
}

type runner struct{}

func (runner runner) Command(name string, arguments ...string) {}

// A method named Command on some other type.
func method() {
	runner{}.Command("go", "test", "-count=1")
}

// A program other than go.
func otherProgram() *osexec.Cmd {
	return osexec.Command("gotest", "test", "-count=1")
}

// The program go run runs takes arguments spelled like go test's, and they are its own.
func goRun() *osexec.Cmd {
	return osexec.Command("go", "run", "./command/tool", "test", "-count=1")
}

// The flag and a 1 that is not its value.
func notAdjacent() *osexec.Cmd {
	return osexec.Command("go", "test", "-count=3", "1")
}

// A flag whose name only starts like count.
func otherFlag() *osexec.Cmd {
	return osexec.Command("go", "test", "-countdown=1", "-cover=1")
}

// The text of the flag, outside any command.
func text() string {
	return fmt.Sprintf("go test %s", "-count=1")
}

// A local reassigned after its declaration holds what the last assignment gave it.
func reassigned() *osexec.Cmd {
	arguments := []string{"test", "-count=1"}
	arguments = []string{"test", "./..."}
	return osexec.Command("go", arguments...)
}

// An element rewritten after the declaration.
func elementRewritten() *osexec.Cmd {
	arguments := []string{"test", "-count=1"}
	arguments[1] = "-race"
	return osexec.Command("go", arguments...)
}

// An address taken lets anything holding it rewrite the slice.
func addressTaken() *osexec.Cmd {
	arguments := []string{"test", "-count=1"}
	rewrite(&arguments)
	return osexec.Command("go", arguments...)
}

func rewrite(arguments *[]string) {
	*arguments = []string{"test"}
}

// The builtin copy writes over the elements.
func copiedOver() *osexec.Cmd {
	arguments := []string{"test", "-count=1"}
	copy(arguments, []string{"test", "-race"})
	return osexec.Command("go", arguments...)
}

// An inner local shadows the outer, and the command spreads the inner one.
func shadowed() (*osexec.Cmd, []string) {
	arguments := []string{"test", "-count=1"}
	outer := arguments
	{
		arguments := []string{"test", "./..."}
		return osexec.Command("go", arguments...), outer
	}
}
