//go:build unix

package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// namedWorktree runs cohere-dev worktree in directory, and returns what it printed on each stream and its
// exit code.
func (fixture *landFixture) namedWorktree(directory string, arguments ...string) (string, string, int) {
	fixture.t.Helper()
	command := exec.Command(fixture.wrapper, append([]string{"worktree"}, arguments...)...)
	command.Dir, command.Env = directory, fixture.environment
	var standardOutput, standardError bytes.Buffer
	command.Stdout, command.Stderr = &standardOutput, &standardError
	err := command.Run()
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		fixture.t.Fatalf("running cohere-dev worktree: %v", err)
	}
	return standardOutput.String(), standardError.String(), command.ProcessState.ExitCode()
}

// namedWorktreePath is where the fixture's worktree of a name is made: beside the checkout of main.
func (fixture *landFixture) namedWorktreePath(name string) string {
	return filepath.Join(filepath.Dir(fixture.repository), worktreePrefix+name)
}

// pinLibrary pins a library of two files into main as a submodule, checked out in the checkout of main, the
// way the land fixture's submodule tests do, and returns its path there.
func (fixture *landFixture) pinLibrary() string {
	fixture.t.Helper()
	library := fixture.t.TempDir()
	fixture.git(library, "init", "-q", "-b", "main")
	fixture.commit(library, "first.txt")
	fixture.commit(library, "second.txt")
	fixture.git(fixture.repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "pin library")
	return filepath.Join(fixture.repository, "library")
}

// sameFile reports whether two paths name the same file, symlinks resolved: git may record a temporary
// directory through /private where the test's path has /var.
func sameFile(t *testing.T, one string, other string) bool {
	t.Helper()
	resolvedOne, errOne := filepath.EvalSymlinks(one)
	resolvedOther, errOther := filepath.EvalSymlinks(other)
	return errOne == nil && errOther == nil && resolvedOne == resolvedOther
}

// A new name makes a worktree at main beside the checkout of main, its submodule a symlink to that checkout's
// rather than a second copy, and prints its path as the only line on standard output, so a caller can cd to
// it. It runs from any worktree of the repository, here a's.
func TestAWorktreeIsMadeAtMainWithItsSubmoduleLinked(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	library := fixture.pinLibrary()
	path := fixture.namedWorktreePath("tools")

	standardOutput, standardError, code := fixture.namedWorktree(fixture.worktree("a"), "tools")
	if code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	lines := strings.Split(strings.TrimSuffix(standardOutput, "\n"), "\n")
	if len(lines) != 1 || !sameFile(t, lines[0], path) {
		t.Errorf("standard output was %q, want %s as its only line", standardOutput, path)
	}
	if !strings.Contains(standardError, "created") {
		t.Errorf("it did not say it created the worktree:\n%s", standardError)
	}
	if head, main := fixture.git(path, "rev-parse", "HEAD"), fixture.git(fixture.repository, "rev-parse", "main"); head != main {
		t.Errorf("the worktree is at %s, want main at %s", head, main)
	}
	info, err := os.Lstat(filepath.Join(path, "library"))
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("the worktree's submodule is not a symlink (%v)", err)
	}
	if !sameFile(t, filepath.Join(path, "library"), library) {
		t.Errorf("the worktree's submodule does not lead to the checkout of main's")
	}
	if _, err := os.Stat(filepath.Join(path, "library", "second.txt")); err != nil {
		t.Errorf("the linked submodule's files are not there: %v", err)
	}
}

// The point of the verb: a worktree moved to a new main has the changed file's new content, and a file the
// move did not change keeps its modification time, which Go's test cache keys it on. Recreating the worktree
// would write every file anew and miss the cache. The worktree's submodule is a symlink, as the team's are,
// and main's pin does not move, so this also shows git switch works in such a tree.
func TestAMovedWorktreeKeepsTheModificationTimesOfFilesThatDidNotChange(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	fixture.pinLibrary()
	fixture.commit(fixture.repository, "changing.txt")
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	// A time no write could give it by accident.
	past := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(path, "base.txt"), past, past); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(fixture.repository, "changing.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.git(fixture.repository, "add", "changing.txt")
	fixture.git(fixture.repository, "commit", "-q", "-m", "change changing.txt")
	standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, "tools")
	if code != 0 {
		t.Fatalf("moving the worktree exited %d:\n%s", code, standardError)
	}
	if !strings.Contains(standardError, "moved") || !strings.Contains(standardError, "1 file changed") {
		t.Errorf("it did not say it moved the worktree and changed one file:\n%s", standardError)
	}
	if !sameFile(t, strings.TrimSpace(standardOutput), path) {
		t.Errorf("standard output was %q, want %s", standardOutput, path)
	}
	if contents, err := os.ReadFile(filepath.Join(path, "changing.txt")); err != nil || string(contents) != "changed\n" {
		t.Errorf("the changed file holds %q (%v), want main's new content", contents, err)
	}
	info, err := os.Stat(filepath.Join(path, "base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Errorf("the file the move did not change was rewritten: its modification time is %s, want %s", info.ModTime(), past)
	}
	if head, main := fixture.git(path, "rev-parse", "HEAD"), fixture.git(fixture.repository, "rev-parse", "main"); head != main {
		t.Errorf("the worktree is at %s, want main at %s", head, main)
	}
	if info, err := os.Lstat(filepath.Join(path, "library")); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the worktree's submodule is no longer a symlink after the move (%v)", err)
	}
}

// A worktree with a changed tracked file, or a file git does not track, is refused by name and left as it
// was: a move would carry the change somewhere it was not made, or bury the file under work it has no part in.
func TestAMoveRefusesADirtyWorktree(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	fixture.commit(fixture.repository, "later.txt")
	before := fixture.git(path, "rev-parse", "HEAD")
	refusedUnchanged := func(file string, contents string) {
		t.Helper()
		standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, "tools")
		if code != 2 || !strings.Contains(standardError, file) || standardOutput != "" {
			t.Errorf("a worktree with %s not committed was not refused by name (exit %d):\n%s%s", file, code, standardOutput, standardError)
		}
		if after := fixture.git(path, "rev-parse", "HEAD"); after != before {
			t.Errorf("the refused worktree moved from %s to %s", before, after)
		}
		if read, err := os.ReadFile(filepath.Join(path, file)); err != nil || string(read) != contents {
			t.Errorf("%s holds %q (%v) after the refusal, want %q", file, read, err, contents)
		}
		if _, err := os.Stat(filepath.Join(path, "later.txt")); err == nil {
			t.Error("the refused worktree has main's later commit's file")
		}
	}

	if err := os.WriteFile(filepath.Join(path, "base.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refusedUnchanged("base.txt", "edited\n")

	if err := os.WriteFile(filepath.Join(path, "base.txt"), []byte("base.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refusedUnchanged("notes.txt", "mine\n")
}

// A worktree holding a commit main does not is refused, the commit named, and left where it was: a move
// would leave the commit reachable from nothing.
func TestAMoveRefusesAWorktreeWithACommitNotOnMain(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	fixture.commit(path, "unlanded.txt")
	unlanded := fixture.git(path, "rev-parse", "--short", "HEAD")
	fixture.commit(fixture.repository, "later.txt")

	standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, "tools")
	if code != 2 || !strings.Contains(standardError, unlanded) || !strings.Contains(standardError, "add unlanded.txt") ||
		!strings.Contains(standardError, "land them") || standardOutput != "" {
		t.Errorf("a worktree with a commit not on main was not refused, naming it (exit %d):\n%s%s", code, standardOutput, standardError)
	}
	if head := fixture.git(path, "rev-parse", "--short", "HEAD"); head != unlanded {
		t.Errorf("the refused worktree moved from %s to %s", unlanded, head)
	}
}

// A worktree whose commit came home to main by cherry-pick, as a lead brings a hand's unit home, moves: the
// commit has a new hash on main, and its patch is there.
func TestAMoveTakesAWorktreeWhoseCommitWasCherryPickedOntoMain(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	fixture.commit(path, "picked.txt")
	fixture.commit(fixture.repository, "later.txt")
	fixture.git(fixture.repository, "cherry-pick", fixture.git(path, "rev-parse", "HEAD"))

	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("a worktree whose commit main holds by cherry-pick was refused (exit %d):\n%s", code, standardError)
	}
	if head, main := fixture.git(path, "rev-parse", "HEAD"), fixture.git(fixture.repository, "rev-parse", "main"); head != main {
		t.Errorf("the worktree is at %s, want main at %s", head, main)
	}
}

// A path that exists and is not one of the repository's worktrees, here another repository, is refused and
// left alone: whatever it is, it is not this verb's to move.
func TestAWorktreeRefusesAPathThatIsNotOneOfTheRepositorys(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	path := fixture.namedWorktreePath("tools")
	fixture.git(filepath.Dir(path), "init", "-q", "-b", "main", path)
	if err := os.WriteFile(filepath.Join(path, "theirs.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, "tools")
	if code != 2 || !strings.Contains(standardError, "is not a worktree of") || standardOutput != "" {
		t.Errorf("a path that is not a worktree of the repository was not refused (exit %d):\n%s%s", code, standardOutput, standardError)
	}
	if contents, err := os.ReadFile(filepath.Join(path, "theirs.txt")); err != nil || string(contents) != "theirs\n" {
		t.Errorf("the refused path's file holds %q (%v)", contents, err)
	}
	if listing := fixture.git(fixture.repository, "worktree", "list"); strings.Contains(listing, worktreePrefix) {
		t.Errorf("the refused path was made a worktree:\n%s", listing)
	}
}

// A name that can't be part of a directory's name, or is the remove subcommand's, is refused, and nothing is
// made.
func TestAWorktreeRefusesABadName(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	for _, arguments := range [][]string{{"Tools"}, {"_tools"}, {"tools/inner"}, {"to ols"}, {"../tools"}, {}, {"--branch", "x", "remove"}, {"remove", "remove"}} {
		standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, arguments...)
		if code != 2 || !strings.Contains(standardError, "usage: cohere-dev worktree") || standardOutput != "" {
			t.Errorf("worktree %q was not refused with exit 2 and the usage (exit %d):\n%s%s", arguments, code, standardOutput, standardError)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(fixture.repository))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), worktreePrefix) {
			t.Errorf("a refused name made %s", entry.Name())
		}
	}
}

// --branch leaves the worktree on a new branch at main, after a make or a move, and a branch that exists
// already is refused in git's own words.
func TestAWorktreeStartsTheBranchItIsGiven(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools", "--branch", "feature"); code != 0 {
		t.Fatalf("making a worktree on a branch exited %d:\n%s", code, standardError)
	}
	onBranchAtMain := func(branch string) {
		t.Helper()
		if current := fixture.git(path, "symbolic-ref", "--short", "HEAD"); current != branch {
			t.Errorf("the worktree is on %q, want %q", current, branch)
		}
		if head, main := fixture.git(path, "rev-parse", "HEAD"), fixture.git(fixture.repository, "rev-parse", "main"); head != main {
			t.Errorf("the worktree is at %s, want main at %s", head, main)
		}
	}
	onBranchAtMain("feature")

	fixture.commit(fixture.repository, "later.txt")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools", "--branch", "feature"); code == 0 ||
		!strings.Contains(standardError, "already exists") {
		t.Errorf("a branch that exists was not refused in git's words (exit %d):\n%s", code, standardError)
	}
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools", "--branch=next"); code != 0 {
		t.Fatalf("moving a worktree onto a new branch exited %d:\n%s", code, standardError)
	}
	onBranchAtMain("next")
}

// remove retires a clean worktree: its directory goes, git no longer lists it, and the checkout of main's
// submodule, which the worktree's symlink led to, keeps every file.
func TestRemoveRetiresACleanWorktreeAndKeepsMainsSubmodule(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	library := fixture.pinLibrary()
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}

	standardOutput, standardError, code := fixture.namedWorktree(fixture.repository, "remove", "tools")
	if code != 0 || !strings.Contains(standardError, "removed") {
		t.Fatalf("removing a clean worktree exited %d:\n%s%s", code, standardOutput, standardError)
	}
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the removed worktree's directory is still there (%v)", err)
	}
	if listing := fixture.git(fixture.repository, "worktree", "list", "--porcelain"); strings.Contains(listing, worktreePrefix+"tools") {
		t.Errorf("git still lists the removed worktree:\n%s", listing)
	}
	for _, file := range []string{"first.txt", "second.txt", ".git"} {
		if _, err := os.Stat(filepath.Join(library, file)); err != nil {
			t.Errorf("the checkout of main's submodule lost %s: %v", file, err)
		}
	}
	if status := fixture.git(library, "status", "--porcelain"); status != "" {
		t.Errorf("the checkout of main's submodule changed:\n%s", status)
	}
}

// remove refuses, as a move does, a worktree with work in it, here a file not committed and then a commit
// not on main, and leaves it in place, its submodule still a symlink.
func TestRemoveRefusesAWorktreeHoldingWork(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	fixture.pinLibrary()
	path := fixture.namedWorktreePath("tools")
	if _, standardError, code := fixture.namedWorktree(fixture.repository, "tools"); code != 0 {
		t.Fatalf("making a worktree exited %d:\n%s", code, standardError)
	}
	refusedInPlace := func(named string) {
		t.Helper()
		_, standardError, code := fixture.namedWorktree(fixture.repository, "remove", "tools")
		if code != 2 || !strings.Contains(standardError, named) {
			t.Errorf("removing a worktree holding work was not refused naming %q (exit %d):\n%s", named, code, standardError)
		}
		if _, err := os.Stat(filepath.Join(path, "base.txt")); err != nil {
			t.Errorf("the refused worktree lost its files: %v", err)
		}
		if info, err := os.Lstat(filepath.Join(path, "library")); err != nil || info.Mode()&fs.ModeSymlink == 0 {
			t.Errorf("the refused worktree's submodule is no longer a symlink (%v)", err)
		}
		if listing := fixture.git(fixture.repository, "worktree", "list", "--porcelain"); !strings.Contains(listing, worktreePrefix+"tools") {
			t.Errorf("git no longer lists the refused worktree:\n%s", listing)
		}
	}

	if err := os.WriteFile(filepath.Join(path, "notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refusedInPlace("notes.txt")

	fixture.git(path, "add", "notes.txt")
	fixture.git(path, "commit", "-q", "-m", "keep my notes")
	refusedInPlace("keep my notes")
}
