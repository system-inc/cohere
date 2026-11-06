// Package allowed builds go commands the rule must leave alone. Nothing here runs.
package allowed

import (
	"context"
	"os/exec"
)

// A cached go test is the point.
func cached(requestContext context.Context) (*exec.Cmd, *exec.Cmd) {
	return exec.Command("go", "test", "./..."), exec.CommandContext(requestContext, "go", "test", "-run", "^TestX$")
}

// A count above one repeats a test to hunt a flake, which is the cure, not the dodge.
func repeated() (*exec.Cmd, *exec.Cmd) {
	return exec.Command("go", "test", "-count=20", "./..."), exec.Command("go", "test", "-count", "5")
}

func otherSubcommands() (*exec.Cmd, *exec.Cmd) {
	return exec.Command("go", "build", "./..."), exec.Command("go", "vet", "./...")
}

func cachedSpread() *exec.Cmd {
	arguments := []string{"test", "-race", "./..."}
	return exec.Command("go", arguments...)
}

// The rest are quiet because the rule cannot tell what reaches the command, and a guess would be a wrong finding.

// A slice built by a call.
func fromCall() *exec.Cmd {
	return exec.Command("go", testArguments()...)
}

func testArguments() []string {
	return []string{"test", "-count=1"}
}

// A slice the caller passes.
func fromParameter(arguments []string) *exec.Cmd {
	return exec.Command("go", arguments...)
}

// A package variable any function in the package may rewrite.
var packageArguments = []string{"test", "-count=1"}

func fromPackageVariable() *exec.Cmd {
	return exec.Command("go", packageArguments...)
}

// A program that is not a constant.
func variableProgram(program string) *exec.Cmd {
	return exec.Command(program, "test", "-count=1")
}

// The count's value that is not a constant.
func variableCount(count string) *exec.Cmd {
	return exec.Command("go", "test", "-count", count)
}

// A first argument that is not a constant, so the subcommand cannot be read.
func variableSubcommand(subcommand string) *exec.Cmd {
	return exec.Command("go", subcommand, "-count=1")
}

// A keyed literal places its elements by index, not in the order written.
func keyedLiteral() *exec.Cmd {
	return exec.Command("go", []string{1: "-count=1", 0: "test"}...)
}

// A reasoned marker excuses the run, directly above its statement or at the end of it.

func reasonAbove() *exec.Cmd {
	// Uncached: the test reads a database the cache cannot see.
	return exec.Command("go", "test", "-count=1")
}

func reasonWrappedAbove() *exec.Cmd {
	// Uncached: the test reaches a service outside its own files,
	// and a cached pass would replay a reply the service no longer gives.
	return exec.Command("go", "test", "-count=1")
}

func reasonAtTheEnd() *exec.Cmd {
	return exec.Command("go", "test", "-count=1") // Uncached: the test reads a database the cache cannot see.
}

func reasonAtTheEndOfManyLines() *exec.Cmd {
	return exec.Command(
		"go", "test", "-count=1",
	) // Uncached: the test reads a database the cache cannot see.
}

func reasonAboveTheCall(requestContext context.Context) (*exec.Cmd, *exec.Cmd) {
	arguments := []string{"test", "-count=1"}
	// Uncached: the test reads a database the cache cannot see.
	return exec.Command("go", arguments...), exec.CommandContext(requestContext, "go", arguments...)
}
