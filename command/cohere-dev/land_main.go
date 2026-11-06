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
// that. From here on, a change to land reaches every landing the moment it is on main.

// landSource is cohere-dev's source, relative to the repository, which decides how land behaves.
const landSource = "command/cohere-dev"

// landWorktreeVariable names the worktree being landed, for main's land run from the main checkout. Its
// presence also says this land is already main's, so it never hands the landing on again.
const landWorktreeVariable = "COHERE_DEV_LAND_WORKTREE"

// mainLandVariable names a command that stands in for `go run ./command/cohere-dev` in the main checkout, so a
// test can run main's land without building cohere's whole module.
const mainLandVariable = "COHERE_DEV_MAIN_LAND"

// landWorktree is the worktree being landed: the one main's land was handed, or the one land runs in.
func landWorktree() (string, error) {
	if handed := os.Getenv(landWorktreeVariable); handed != "" {
		return handed, nil
	}
	return gitOutput("", "rev-parse", "--show-toplevel")
}

// runsMainsLand reports whether this land is main's, so the landing is its own: it was handed the landing, or
// the worktree's cohere-dev is main's, or the repository has no cohere-dev source at all, so there is no other
// land to run.
func runsMainsLand(worktree string) bool {
	if os.Getenv(landWorktreeVariable) != "" {
		return true
	}
	mains, gitOutputError := gitOutput(worktree, "rev-parse", "--verify", "--quiet", "main:"+landSource)
	if gitOutputError != nil {
		return true
	}
	// The source that is running: land refuses a worktree with changes, so its committed tree is its disk.
	// Ignored: a head with no cohere-dev source differs from main's, and main's land is the one to run.
	own, _ := gitOutput(worktree, "rev-parse", "--verify", "--quiet", "HEAD:"+landSource)
	return own == mains
}

// runMainsLand runs main's land from the main checkout against worktree, with the same arguments, and returns
// its exit code. A main checkout whose cohere-dev differs from main's commit would run neither the lander's
// land nor main's, so it is refused.
func runMainsLand(worktree string, mainCheckout string, arguments []string) int {
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
		landSource,
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
