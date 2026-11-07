package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/system-inc/cohere/internal/streams"
)

// Every landing runs main's own land, whatever the lander's worktree carries (#8wpk5tp). land runs from the
// lander's worktree, as `go run ./command/cohere-dev land`, so a worktree on old code landed the old way after
// land changed: 50f70d03, between the first two landings of #4v9pmef, merged main into its branch and
// fast-forwarded main onto it, which put d1f79a56 off main's first-parent line. Now a land whose own source
// differs from main's runs main's from the main checkout, against the lander's worktree, and says so.
//
// A worktree whose code predates this file can't be reached by it: only moving the worktree to main does
// that. From here on, a change to land reaches every landing the moment it is on main. Land's source is everything
// it links (tree_listing.go), since #j3bfnyt changed a package the gate calls and no landing ran it.

// landSource is cohere-dev's source, relative to the repository, which decides how land behaves.
const landSource = "command/cohere-dev"

// landMainFile is this file, whose presence on main says main's land takes a landing handed to it.
const landMainFile = "land_main.go"

// landWorktreeVariable names the worktree being landed, for main's land run from the main checkout. Its
// presence also says this land is already main's, so it never hands the landing on again.
const landWorktreeVariable = "COHERE_DEV_LAND_WORKTREE"

// mainLandVariable names a command that stands in for `go run ./command/cohere-dev` in the main checkout, so a
// test can run main's land without building cohere's whole module.
const mainLandVariable = "COHERE_DEV_MAIN_LAND"

// handedWorktree is the worktree main's land was handed, read once and taken out of the environment by
// takeHandedWorktree, or empty for a land run where it lands.
var handedWorktree string

// takeHandedWorktree reads the worktree this land was handed and removes it from the environment, so nothing
// land starts inherits it. The gate's tests did: under main's land, each test's own `cohere-dev land` read the
// variable and took the real worktree being landed for its fixture's (#06g3f7x's first landing, 2026-11-06).
func takeHandedWorktree() {
	handedWorktree = os.Getenv(landWorktreeVariable)
	// Ignored: Unsetenv fails only on a name with a zero byte or an equals sign, and this holds neither.
	_ = os.Unsetenv(landWorktreeVariable)
}

// landWorktree is the worktree being landed: the one main's land was handed, or the one land runs in.
func landWorktree() (string, error) {
	if handedWorktree != "" {
		return handedWorktree, nil
	}
	return gitOutput("", "rev-parse", "--show-toplevel")
}

// runsMainsLand reports whether this land is main's, so the landing is its own: it was handed the landing, or
// everything the worktree's land links is main's, or the repository has no cohere-dev source at all, so there is
// no other land to run. When it is not, it also names the first input that differs.
func runsMainsLand(worktree string) (bool, string) {
	if handedWorktree != "" {
		return true, ""
	}
	_, gitOutputError := gitOutput(worktree, "rev-parse", "--verify", "--quiet", "main:"+landSource)
	if gitOutputError != nil {
		return true, ""
	}
	// A main whose land predates this file reads the worktree from where it runs, so it would refuse to land the
	// main checkout itself, and its landing is this land's: the first landing of this file was one.
	mainTakesOne := "main:" + landSource + "/" + landMainFile
	if _, gitOutputError := gitOutput(worktree, "cat-file", "-e", mainTakesOne); gitOutputError != nil {
		return true, ""
	}
	// The source that is running: land refuses a worktree with changes, so its committed tree is its disk.
	differs := landInputsDiffer(worktree)
	return differs == "", differs
}

// runMainsLand runs main's land from the main checkout against worktree, with the same arguments, and returns
// its exit code. A main checkout whose cohere-dev differs from main's commit would run neither the lander's
// land nor main's, so it is refused.
func runMainsLand(worktree string, mainCheckout string, differs string, arguments []string) int {
	if _, gitOutputError := gitOutput(
		mainCheckout,
		"diff",
		"--quiet",
		"HEAD",
		"--",
		landSource,
	); gitOutputError != nil {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: this worktree's land differs from main's, and the main checkout %s has changes to %s, so "+
				"neither is main's land: commit or remove them there, then land again\n",
			mainCheckout,
			landSource,
		)
		return 2
	}
	fmt.Fprintf(
		streams.Errors,
		"cohere-dev: this worktree's %s differs from main's, so main's land runs, from %s\n",
		differs,
		mainCheckout,
	)
	command := exec.Command("go", append([]string{"run", "./" + landSource, "land"}, arguments...)...)
	if standIn := os.Getenv(mainLandVariable); standIn != "" {
		command = exec.Command(standIn, append([]string{"land"}, arguments...)...)
	}
	command.Dir = mainCheckout
	command.Env = append(os.Environ(), landWorktreeVariable+"="+worktree)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	// What this land said first reaches the terminal before main's land writes to it.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if runError := command.Run(); runError != nil {
		var exited *exec.ExitError
		if errors.As(runError, &exited) {
			return exited.ExitCode()
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: running main's land: %v\n", runError)
		return 1
	}
	return 0
}
