package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/streams"
)

// cohere-dev worktree: one worktree per name at a path that stays, moved between units of work with git, so
// Go's test cache keeps its hits.
//
// Go's test cache keys every file a test opens on its path, size, mode and modification time, not its
// content (hashOpen in cmd/go's test.go). The team made a fresh worktree for each unit of work, so nearly
// every test result missed: a fresh worktree's first fast run took 65s and 293 CPU-s with 45 of 95 packages
// cached, and a reused one moved to the new commit with git took 28s and 132 CPU-s with 79 cached. git
// rewrites only the files that changed and leaves every other file's modification time alone. Recreating a
// worktree at the same path is no better than a fresh one, since every file it writes is new.
//
// So a name is a hand's, not a member's: a lead fanning out several hands at once gives each its own, such
// as system_cohere_tools_1, and each keeps its worktree from one unit of work to the next. Moving one never
// loses work: it refuses a tree with changes or with commits main does not hold, and moves with git switch,
// never with a reset, a stash, a clean or a remove and re-add. remove retires one under the same refusals.
//
// It runs only git, so it takes no pool token.

// worktreeNamePattern is what a worktree's name may be: it becomes part of a directory's name.
var worktreeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// worktreePrefix begins the directory name of every named worktree, beside the checkout of main.
const worktreePrefix = "cohere-wt-"

// removeSubcommand retires a named worktree, so it is not a name one may have.
const removeSubcommand = "remove"

// worktreeUsage is said with every refusal of the verb's arguments.
const worktreeUsage = "usage: cohere-dev worktree <name> [--branch <branch>]\n" +
	"       cohere-dev worktree remove <name>\n" +
	"A name is per hand, not per member: a lead fanning out several hands at once gives each its own, " +
	"such as system_cohere_tools_1."

// namedWorktree makes the named worktree at main, or moves it there, and prints its path as the only line
// on standard output, so a caller can cd to it. With --branch it then starts that branch in it.
func namedWorktree(arguments []string) int {
	if len(arguments) > 0 && arguments[0] == removeSubcommand {
		return removeNamedWorktree(arguments[1:])
	}
	name, branch, branchGiven := "", "", false
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--branch" && index+1 < len(arguments):
			index++
			branch, branchGiven = arguments[index], true
		case strings.HasPrefix(argument, "--branch="):
			branch, branchGiven = strings.TrimPrefix(argument, "--branch="), true
		case name == "" && !strings.HasPrefix(argument, "-"):
			name = argument
		default:
			fmt.Fprintf(streams.Errors, "cohere-dev: worktree does not take %q\n%s\n", argument, worktreeUsage)
			return 2
		}
	}
	if checkWorktreeNameError := checkWorktreeName(name); checkWorktreeNameError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n%s\n", checkWorktreeNameError, worktreeUsage)
		return 2
	}
	// A branch that began with a dash would reach git switch as a flag.
	if branchGiven && (branch == "" || strings.HasPrefix(branch, "-")) {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: --branch takes a branch name, and %q is not one\n%s\n",
			branch,
			worktreeUsage,
		)
		return 2
	}
	mainCheckout, path, code := namedWorktreePath(name)
	if code != 0 {
		return code
	}

	if _, statError := os.Lstat(path); errors.Is(statError, fs.ErrNotExist) {
		if code := createNamedWorktree(mainCheckout, path); code != 0 {
			return code
		}
	} else if statError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s: %v\n", path, statError)
		return 1
	} else if code := moveNamedWorktree(mainCheckout, path); code != 0 {
		return code
	}

	if branch != "" {
		if output, gitCombinedError := gitCombined(path, "switch", "-c", branch); gitCombinedError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: starting branch %s in %s: %v\n%s\n",
				branch,
				path,
				gitCombinedError,
				output,
			)
			return 1
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is on a new branch %s\n", path, branch)
	}
	// Its notes first, as they were when each print went out at once, then the path a caller reads.
	streams.Show(streams.Errors)
	fmt.Fprintln(streams.Output, path)
	return 0
}

// checkWorktreeName refuses a name that can't be a worktree's.
func checkWorktreeName(name string) error {
	if name == removeSubcommand {
		return fmt.Errorf("%q can't be a worktree's name: cohere-dev worktree remove <name> retires one", name)
	}
	if !worktreeNamePattern.MatchString(name) {
		return fmt.Errorf("%q can't be a worktree's name: a name is lowercase letters, digits, _ and -, "+
			"beginning with a letter or a digit (%s)", name, worktreeNamePattern)
	}
	return nil
}

// namedWorktreePath finds the checkout of main of the repository holding the working directory, and the
// named worktree's path beside it. A nonzero code is the exit code of a failure it has said.
func namedWorktreePath(name string) (string, string, int) {
	top, gitOutputError := gitOutput("", "rev-parse", "--show-toplevel")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: worktree runs inside a checkout of cohere: %v\n", gitOutputError)
		return "", "", 2
	}
	mainCheckout, checkoutOfMainError := checkoutOfMain(top)
	if checkoutOfMainError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", checkoutOfMainError)
		return "", "", 1
	}
	return mainCheckout, filepath.Join(filepath.Dir(mainCheckout), worktreePrefix+name), 0
}

// createNamedWorktree adds a worktree at main, its submodules linked to the checkout of main's.
func createNamedWorktree(mainCheckout string, path string) int {
	if output, gitCombinedError := gitCombined(
		mainCheckout,
		"worktree",
		"add",
		"--detach",
		path,
		"main",
	); gitCombinedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: adding a worktree at %s: %v\n%s\n", path, gitCombinedError, output)
		return 1
	}
	if linkSubmodulesError := linkSubmodules(mainCheckout, path); linkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", linkSubmodulesError)
		return 1
	}
	// Ignored: head only names the commit in the line below; the worktree is made either way.
	head, _ := gitOutput(path, "rev-parse", "HEAD")
	fmt.Fprintf(streams.Errors, "cohere-dev: created %s at main, %s\n", path, shortCommit(head))
	return 0
}

// moveNamedWorktree moves an existing worktree to main with git switch, which rewrites only the files that
// differ, so every other file keeps the modification time Go's test cache keyed it on. It refuses, changing
// nothing, a tree a move would lose work from.
func moveNamedWorktree(mainCheckout string, path string) int {
	if code := checkWorktreeHoldsNoWork(mainCheckout, path, "moving"); code != 0 {
		return code
	}
	before, gitOutputError := gitOutput(path, "rev-parse", "HEAD")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s's head: %v\n", path, gitOutputError)
		return 1
	}
	if output, gitCombinedError := gitCombined(path, "switch", "--detach", "main"); gitCombinedError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: moving %s to main: %v\n%s\n", path, gitCombinedError, output)
		return 1
	}
	// git replaces a symlinked submodule with an empty directory when the move changes its pin, and adds one
	// for a submodule main gained.
	if linkSubmodulesError := linkSubmodules(mainCheckout, path); linkSubmodulesError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", linkSubmodulesError)
		return 1
	}
	after, gitOutputError := gitOutput(path, "rev-parse", "HEAD")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s's head: %v\n", path, gitOutputError)
		return 1
	}
	changed, gitOutputError := gitOutput(path, "diff", "--name-only", before, after)
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: listing the files the move changed: %v\n", gitOutputError)
		return 1
	}
	count := 0
	if changed != "" {
		count = len(strings.Split(changed, "\n"))
	}
	files := "files"
	if count == 1 {
		files = "file"
	}
	fmt.Fprintf(
		streams.Errors,
		"cohere-dev: moved %s from %s to %s, %d %s changed\n",
		path,
		shortCommit(before),
		shortCommit(after),
		count,
		files,
	)
	return 0
}

// checkWorktreeHoldsNoWork refuses, saying what it found, a path that is not a worktree of the repository
// besides the checkout of main, or one with changes or untracked files, or with commits main does not hold:
// what a move or a removal would lose. doing names the act refused. A nonzero code is the exit code of a
// refusal or a failure it has said.
func checkWorktreeHoldsNoWork(mainCheckout string, path string, doing string) int {
	registered, isWorktreeOfError := isWorktreeOf(mainCheckout, path)
	if isWorktreeOfError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", isWorktreeOfError)
		return 1
	}
	if !registered {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s exists and is not a worktree of %s's repository, so it is not %s "+
			"it; move it aside or pick another name\n", path, mainCheckout, doing)
		return 2
	}
	// Submodules are left out, since git status refuses a worktree whose submodule is a symlink, as the team
	// makes them. Untracked files count: they are work, and a removal would take them.
	changes, gitOutputError := gitOutput(path, "status", "--porcelain", "--ignore-submodules=all")
	if gitOutputError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s's status: %v\n", path, gitOutputError)
		return 1
	}
	if changes != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s has changes or untracked files, so it is not %s it; commit them "+
			"or take them out:\n%s\n", path, doing, changes)
		return 2
	}
	// A commit whose patch main holds counts as landed: a lead brings a hand's unit home by cherry-pick, which
	// gives it a new hash on main, and main..`HEAD` would refuse that hand's worktree for good. A patch that
	// differs at all, a conflict resolved, still counts as unlanded, which keeps the refusal on the safe side.
	unlanded, gitOutputError := gitOutput(path, "log", "--oneline", "--cherry-pick", "--right-only", "main...HEAD")
	if gitOutputError != nil {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: reading %s's commits that main does not hold: %v\n",
			path,
			gitOutputError,
		)
		return 1
	}
	if unlanded != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s holds commits that main does not, so it is not %s it; land them "+
			"(cohere-dev land), or keep them on a branch in another worktree:\n%s\n", path, doing, unlanded)
		return 2
	}
	return 0
}

// isWorktreeOf reports whether path is one of the repository's worktrees other than the checkout of main.
// Both sides are compared with symlinks resolved, since git may record a path through /private where the
// caller's has /tmp.
func isWorktreeOf(mainCheckout string, path string) (bool, error) {
	listing, gitOutputError := gitOutput(mainCheckout, "worktree", "list", "--porcelain")
	if gitOutputError != nil {
		return false, fmt.Errorf("listing the repository's worktrees: %w", gitOutputError)
	}
	resolved, resolveSymlinksError := filepath.EvalSymlinks(path)
	if resolveSymlinksError != nil {
		return false, resolveSymlinksError
	}
	if resolvedMain, resolveSymlinksError := filepath.EvalSymlinks(
		mainCheckout,
	); resolveSymlinksError == nil && resolvedMain == resolved {
		return false, nil
	}
	for _, line := range strings.Split(listing, "\n") {
		listed, isWorktree := strings.CutPrefix(line, "worktree ")
		if !isWorktree {
			continue
		}
		if resolvedListed, resolveSymlinksError := filepath.EvalSymlinks(
			listed,
		); resolveSymlinksError == nil && resolvedListed == resolved {
			return true, nil
		}
	}
	return false, nil
}

// linkSubmodules replaces each of the worktree's empty submodule directories with a symlink to the checkout
// of main's, when that one is populated, as the team makes every worktree: TypeScript is too big to check out
// again for each one.
func linkSubmodules(mainCheckout string, worktree string) error {
	links, gitlinksOfError := project.GitlinksOf(worktree)
	if gitlinksOfError != nil {
		return gitlinksOfError
	}
	for _, link := range links {
		directory := filepath.Join(worktree, link.Path)
		source := filepath.Join(mainCheckout, link.Path)
		info, statError := os.Lstat(directory)
		if statError != nil || !info.IsDir() {
			continue
		}
		if entries, readDirectoryError := os.ReadDir(directory); readDirectoryError != nil || len(entries) > 0 {
			continue
		}
		if entries, readDirectoryError := os.ReadDir(source); readDirectoryError != nil || len(entries) == 0 {
			continue
		}
		// The directory is empty, so this removes nothing but it.
		if removeError := os.Remove(directory); removeError != nil {
			return fmt.Errorf("making way for %s's submodule %s: %w", worktree, link.Path, removeError)
		}
		if symlinkError := os.Symlink(source, directory); symlinkError != nil {
			return fmt.Errorf("linking %s's submodule %s to %s: %w", worktree, link.Path, source, symlinkError)
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: linked %s to %s\n", directory, source)
	}
	return nil
}

// removeNamedWorktree retires a named worktree, refusing as a move does, and changing nothing when it does.
func removeNamedWorktree(arguments []string) int {
	if len(arguments) != 1 {
		fmt.Fprintf(streams.Errors, "cohere-dev: worktree remove takes one name\n%s\n", worktreeUsage)
		return 2
	}
	name := arguments[0]
	if checkWorktreeNameError := checkWorktreeName(name); checkWorktreeNameError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n%s\n", checkWorktreeNameError, worktreeUsage)
		return 2
	}
	mainCheckout, path, code := namedWorktreePath(name)
	if code != 0 {
		return code
	}
	if _, statError := os.Lstat(path); errors.Is(statError, fs.ErrNotExist) {
		fmt.Fprintf(streams.Errors, "cohere-dev: there is no worktree named %s, at %s, to remove\n", name, path)
		return 2
	} else if statError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: reading %s: %v\n", path, statError)
		return 1
	}
	if code := checkWorktreeHoldsNoWork(mainCheckout, path, "removing"); code != 0 {
		return code
	}

	// git refuses to remove a worktree holding a submodule, and a symlink to the checkout of main's reads as
	// one. Taken out, it reads as a deletion, and git wants --force, which would also drop git's own check
	// for changes. An empty directory is what git leaves for a submodule never checked out, and it removes
	// that plainly. So each symlink becomes one first, and a submodule checked out in the worktree itself,
	// which holds work of its own that the checks above did not look at, is refused.
	links, gitlinksOfError := project.GitlinksOf(path)
	if gitlinksOfError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", gitlinksOfError)
		return 1
	}
	symlinks := map[string]string{}
	for _, link := range links {
		directory := filepath.Join(path, link.Path)
		info, statError := os.Lstat(directory)
		if errors.Is(statError, fs.ErrNotExist) {
			continue
		}
		if statError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: reading %s: %v\n", directory, statError)
			return 1
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, readLinkError := os.Readlink(directory)
			if readLinkError != nil {
				fmt.Fprintf(streams.Errors, "cohere-dev: reading %s: %v\n", directory, readLinkError)
				return 1
			}
			symlinks[directory] = target
			continue
		}
		if entries, readDirectoryError := os.ReadDir(directory); readDirectoryError != nil || len(entries) > 0 {
			fmt.Fprintf(streams.Errors, "cohere-dev: %s's submodule %s is checked out in it, not linked, and may hold "+
				"work of its own, so it is not removing it; remove that worktree by hand\n", path, link.Path)
			return 2
		}
	}
	for directory := range symlinks {
		// os.Remove of a symlink unlinks the link itself and never follows it, so the checkout of main's
		// submodule it points to is untouched.
		if removeError := os.Remove(directory); removeError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: unlinking %s: %v\n", directory, removeError)
			return 1
		}
		if createDirectoryError := os.Mkdir(directory, 0o755); createDirectoryError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: replacing %s with an empty directory: %v\n",
				directory,
				createDirectoryError,
			)
			relinkSubmodules(symlinks)
			return 1
		}
	}
	if output, gitCombinedError := gitCombined(mainCheckout, "worktree", "remove", path); gitCombinedError != nil {
		relinkSubmodules(symlinks)
		fmt.Fprintf(streams.Errors, "cohere-dev: removing %s: %v\n%s\n", path, gitCombinedError, output)
		return 1
	}
	if output, gitCombinedError := gitCombined(mainCheckout, "worktree", "prune"); gitCombinedError != nil {
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: removed %s, and pruning the repository's worktrees failed: %v\n%s\n",
			path,
			gitCombinedError,
			output,
		)
		return 1
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: removed %s\n", path)
	return 0
}

// relinkSubmodules puts back the symlinks a removal that failed took out, so the worktree is as it was.
func relinkSubmodules(symlinks map[string]string) {
	for directory, target := range symlinks {
		if info, statError := os.Lstat(directory); statError == nil && info.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		// Ignored: a directory that won't go makes the symlink below fail, and that failure is said.
		_ = os.Remove(directory)
		if symlinkError := os.Symlink(target, directory); symlinkError != nil {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: putting back %s, a symlink to %s: %v\n",
				directory,
				target,
				symlinkError,
			)
		}
	}
}
