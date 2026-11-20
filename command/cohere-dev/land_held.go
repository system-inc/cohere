package main

import (
	"fmt"
	"strings"
)

// A landing main already holds gates nothing new, so land refuses it before any gate runs (#mk56rcq). On Nov 20 a
// head joined the line while main lacked it, the landing ahead of it in the line landed the same head, and the second
// then merged it onto a main that already held it and gated an empty merge for 33 minutes on Sun2. Land asks twice:
// before it takes its place in the line, and again after each merge onto main as main stands then, since what lands
// while it waits is what makes a landing empty.

// heldByMain says why main at mainCommit already holds the landing of head, whose merge onto it is tree, or "" when it
// doesn't: head is main's ancestor, landed by the first commit on main that holds it, or the merge changes nothing,
// every change head makes being on main already by another road (a cherry-pick, a train).
func heldByMain(worktree string, mainCommit string, head string, tree string) (string, error) {
	// Ignored: a head that isn't main's ancestor makes merge-base exit 1, and one it can't read makes it fail the
	// same way; the tree comparison below reads both commits again and says so in git's words.
	if _, ancestorError := gitOutput(worktree, "merge-base", "--is-ancestor", head, mainCommit); ancestorError == nil {
		carriers, logError := gitOutput(worktree, "log", "--first-parent", "--ancestry-path", "--reverse",
			"--format=%h %s", head+".."+mainCommit)
		if logError != nil {
			return "", logError
		}
		carrier, _, _ := strings.Cut(carriers, "\n")
		if carrier == "" {
			return fmt.Sprintf("main is %s itself", shortCommit(head)), nil
		}
		return fmt.Sprintf("main already holds %s, landed by %s", shortCommit(head), carrier), nil
	}
	mainTree, treeError := gitOutput(worktree, "rev-parse", mainCommit+"^{tree}")
	if treeError != nil {
		return "", treeError
	}
	if tree == mainTree {
		return fmt.Sprintf("merging %s onto main at %s changes nothing: main already holds every change it makes",
			shortCommit(head), shortCommit(mainCommit)), nil
	}
	return "", nil
}
