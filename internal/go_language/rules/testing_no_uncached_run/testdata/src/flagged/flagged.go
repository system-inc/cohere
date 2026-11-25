// Package flagged builds go test commands that pass -count=1, each of which the rule must report. Nothing here runs.
package flagged

import (
	"context"
	osexec "os/exec"
)

const goProgram = "go"

const uncached = "-count=1"

func inline() *osexec.Cmd {
	return osexec.Command("go", "test", "./...", "-count=1") // want "throws away the test cache"
}

func withContext(requestContext context.Context) *osexec.Cmd {
	return osexec.CommandContext(requestContext, "go", "test", "-count", "1", "./...") // want "throws away the test"
}

func testBinaryFlag() *osexec.Cmd {
	return osexec.Command("go", "test", "-test.count=1", "./...") // want "throws away the test cache"
}

func doubleDash() *osexec.Cmd {
	return osexec.Command("go", "test", "--count=1", "./...") // want "throws away the test cache"
}

func namedConstants() *osexec.Cmd {
	return osexec.Command(goProgram, "test", uncached) // want "throws away the test cache"
}

// A package path the rule cannot read holds its place, and the flag after it is still a constant.
func variablePackage(packagePath string) *osexec.Cmd {
	return osexec.Command("go", "test", packagePath, "-count=1") // want "throws away the test cache"
}

func inAnotherDirectory() *osexec.Cmd {
	return osexec.Command("go", "-C", "module", "test", "-count=1") // want "throws away the test cache"
}

func inAnotherDirectoryJoined() *osexec.Cmd {
	return osexec.Command("go", "-C=module", "test", "-count=1") // want "throws away the test cache"
}

func spreadLiteral() *osexec.Cmd {
	return osexec.Command("go", []string{"test", "./...", "-count=1"}...) // want "throws away the test cache"
}

func spreadLocal() *osexec.Cmd {
	arguments := []string{"test", "./...", "-count=1"} // want "throws away the test cache"
	return osexec.Command("go", arguments...)
}

func spreadDeclaredLocal() *osexec.Cmd {
	var arguments = []string{"test", "-count", "1"} // want "throws away the test cache"
	return osexec.Command("go", arguments...)
}

func appendedInCall() *osexec.Cmd {
	return osexec.Command("go", append([]string{"test", "./..."}, "-count=1")...) // want "throws away the test cache"
}

func appendedInDefinition() *osexec.Cmd {
	arguments := append([]string{"test", "./..."}, uncached) // want "throws away the test cache"
	return osexec.Command("go", arguments...)
}

func appendedToLocal() *osexec.Cmd {
	base := []string{"test", "./..."}
	return osexec.Command("go", append(base, "-count=1")...) // want "throws away the test cache"
}

// What append spreads holds its place even when the rule cannot read it, since it cannot take the flag away.
func appendedUnreadable(packages []string) *osexec.Cmd {
	arguments := append(append([]string{"test"}, packages...), "-count=1") // want "throws away the test cache"
	return osexec.Command("go", arguments...)
}

// Two commands spread the same literal, and its one -count=1 is reported once.
func spreadTwice(requestContext context.Context) (*osexec.Cmd, *osexec.Cmd) {
	arguments := []string{"test", "-count=1"} // want "throws away the test cache"
	return osexec.Command("go", arguments...), osexec.CommandContext(requestContext, "go", arguments...)
}

// A closure building the command reads the local its function declared.
func inClosure() func() *osexec.Cmd {
	arguments := []string{"test", "-count=1"} // want "throws away the test cache"
	return func() *osexec.Cmd {
		return osexec.Command("go", arguments...)
	}
}

// Reading the local, to log it or measure it, leaves its value as declared.
func readLocal() (*osexec.Cmd, int) {
	arguments := []string{"test", "-count=1"} // want "throws away the test cache"
	return osexec.Command("go", arguments...), len(arguments)
}

// A marker with nothing after its colon excuses nothing and asks for the reason.
func emptyReason() *osexec.Cmd {
	// Uncached:
	return osexec.Command("go", "test", "-count=1") // want "gives no reason"
}

// A sentence that only mentions the marker excuses nothing.
func markerMentioned() *osexec.Cmd {
	// This run was Uncached: once, to chase a flake.
	return osexec.Command("go", "test", "-count=1") // want "throws away the test cache"
}

// A comment trailing the code on the line above speaks for that code.
func markerTrailsLineAbove() (*osexec.Cmd, string) {
	program := "go"                                          // Uncached: this line's reason, not the next's
	return osexec.Command("go", "test", "-count=1"), program // want "throws away the test cache"
}

// A comment before the code on the line above speaks for that code too.
func markerBeforeCodeAbove() (*osexec.Cmd, string) {
	/* Uncached: this line's reason, not the next's. */ program := "go"
	return osexec.Command("go", "test", "-count=1"), program // want "throws away the test cache"
}
