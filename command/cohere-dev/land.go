package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cohere-dev land: a merge queue, so a green gate can't be starved by main moving under it (#fz6xejy).
//
// Landing used to be: gate the branch, then fast-forward main if it hadn't moved. A whole-module gate took
// 7 to 10 minutes on a loaded machine, and others fast-forwarded main during it, so a branch could gate
// green eight times in an hour and never land (#sycrdr6), and every one of those gates compiled a merge
// that never existed again, much of what churned the Go build cache.
//
// Now a landing takes the machine's one land lock, in arrival order, and holds it from the merge to the
// fast-forward: it merges current main into the worktree being landed, gates the result through a pool
// token, and fast-forwards main to it. Nothing that lands this way can move main during another's gate. If
// main moved anyway, by a fast-forward outside land, the fast-forward is refused by git and land merges and
// gates again.

// landAttempts is how many times land merges and gates before giving up on a main that keeps moving
// outside it.
const landAttempts = 3

// land lands the commit the working directory's worktree is at onto main, and returns its exit code.
func land(arguments []string) int {
	if len(arguments) > 0 {
		fmt.Fprintln(os.Stderr, "cohere-dev: land takes no arguments: it lands the commit this worktree is at onto main")
		return 2
	}
	worktree, err := gitOutput("", "rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: land runs inside the worktree to land: %v\n", err)
		return 2
	}
	// Tracked changes would be left behind by a landing of commits. Untracked files are the worktree's own.
	if changes, err := gitOutput(worktree, "status", "--porcelain", "--untracked-files=no"); err != nil || changes != "" {
		fmt.Fprintf(os.Stderr, "cohere-dev: land lands commits, and %s has changes that are not committed:\n%s\n", worktree, changes)
		return 2
	}
	mainCheckout, err := checkoutOfMain(worktree)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}
	if mainCheckout == worktree {
		fmt.Fprintln(os.Stderr, "cohere-dev: land runs in the worktree being landed, not in the checkout of main itself")
		return 2
	}

	directory, err := landDirectory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}
	head, _ := gitOutput(worktree, "rev-parse", "--short", "HEAD")
	held, err := waitInLine(directory, 1, "landing "+head+" from "+worktree, "land lock", func() {
		fmt.Fprintln(os.Stderr, "cohere-dev: waiting to land; one landing runs at a time, merge to fast-forward, and this one holds it:")
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
		return 1
	}
	defer held.release()

	for attempt := 1; attempt <= landAttempts; attempt++ {
		mainCommit, err := gitOutput(worktree, "rev-parse", "main")
		if err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: reading main: %v\n", err)
			return 1
		}
		if err := mergeMain(worktree, mainCommit); err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
			return 1
		}
		landing, _ := gitOutput(worktree, "rev-parse", "HEAD")
		if code := gate(worktree, landing); code != 0 {
			fmt.Fprintf(os.Stderr, "cohere-dev: the gate failed, so nothing landed; %s is merged with main at %s for you to fix\n",
				worktree, shortCommit(mainCommit))
			return code
		}
		// git refuses a fast-forward when main has moved since the merge, which is the test that nothing
		// landed outside this lock during the gate.
		if output, err := gitCombined(mainCheckout, "merge", "--ff-only", landing); err != nil {
			moved, _ := gitOutput(worktree, "rev-parse", "main")
			if moved != mainCommit {
				fmt.Fprintf(os.Stderr, "cohere-dev: main moved outside land during the gate, %s to %s; merging and gating again\n",
					shortCommit(mainCommit), shortCommit(moved))
				continue
			}
			fmt.Fprintf(os.Stderr, "cohere-dev: fast-forwarding main in %s: %v\n%s\n", mainCheckout, err, output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "cohere-dev: landed %s on main\n", shortCommit(landing))
		return 0
	}
	fmt.Fprintf(os.Stderr, "cohere-dev: main moved outside land during each of %d gates, so nothing landed\n", landAttempts)
	return 1
}

// landDirectory holds the land lock and its line, beside the pool's.
func landDirectory() (string, error) {
	pool, err := poolDirectory()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(pool, "land")
	return directory, os.MkdirAll(directory, 0o755)
}

// checkoutOfMain is the worktree that has main checked out, whose working tree a fast-forward must move
// along with the branch.
func checkoutOfMain(worktree string) (string, error) {
	listing, err := gitOutput(worktree, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("listing the repository's worktrees: %w", err)
	}
	path := ""
	for _, line := range strings.Split(listing, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/main":
			return path, nil
		}
	}
	return "", errors.New("no worktree of this repository has main checked out, so there is no main to land on")
}

// mergeMain merges main into the worktree, unless it already holds it. A merge that conflicts is undone,
// leaving the worktree as it was, and says which files conflicted.
func mergeMain(worktree string, mainCommit string) error {
	if _, err := gitOutput(worktree, "merge-base", "--is-ancestor", mainCommit, "HEAD"); err == nil {
		return nil
	}
	output, err := gitCombined(worktree, "merge", "--no-edit", "-m", "Merge main into the work being landed", mainCommit)
	if err == nil {
		return nil
	}
	conflicts, _ := gitOutput(worktree, "diff", "--name-only", "--diff-filter=U")
	gitCombined(worktree, "merge", "--abort")
	if conflicts != "" {
		return fmt.Errorf("main at %s conflicts with this work in:\n%s\nmerge main and resolve them, then land again",
			shortCommit(mainCommit), conflicts)
	}
	return fmt.Errorf("merging main at %s: %v\n%s", shortCommit(mainCommit), err, output)
}

// gate runs the landing gate on the worktree through a pool token: go vet and the whole module's tests.
func gate(worktree string, commit string) int {
	return withToken("land gate on "+shortCommit(commit)+" in "+worktree, func(environment []string) int {
		vet := exec.Command("go", "vet", "./...")
		vet.Dir, vet.Env = worktree, environment
		vet.Stdout, vet.Stderr = os.Stdout, os.Stderr
		if err := vet.Run(); err != nil {
			return exitCodeOf(err)
		}
		// The same environment cohere-dev test gives the module's tests, corpora included (corpora.go).
		testing, uncovered, err := testEnvironment(environment)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
			return 2
		}
		defer printUncovered(os.Stderr, uncovered)
		test := exec.Command("go", "test", "./...")
		test.Dir, test.Env = worktree, testing
		test.Stdout, test.Stderr = os.Stdout, os.Stderr
		if err := test.Run(); err != nil {
			return exitCodeOf(err)
		}
		return 0
	})
}

func exitCodeOf(err error) int {
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		return exited.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "cohere-dev: %v\n", err)
	return 1
}

// gitOutput runs git in a directory, the working directory when it is empty, and returns its trimmed output.
func gitOutput(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, err := command.Output()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(standardError.String()))
	}
	return strings.TrimSpace(string(output)), nil
}

// gitCombined runs git in a directory and returns everything it printed.
func gitCombined(directory string, arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
