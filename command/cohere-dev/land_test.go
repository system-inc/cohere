//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// landFixture is a repository with main checked out and two worktrees, a and b, each one commit ahead on a
// file of its own, and a cohere-dev whose go stands in for the gate: it logs when each worktree's test run
// starts and ends, and which files the tree it tests holds.
type landFixture struct {
	t           *testing.T
	wrapper     string
	repository  string
	log         string
	environment []string
}

func newLandFixture(t *testing.T) *landFixture {
	t.Helper()
	fixture := &landFixture{t: t, wrapper: filepath.Join(t.TempDir(), "cohere-dev"), repository: t.TempDir()}
	if output, err := exec.Command("go", "build", "-o", fixture.wrapper, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	bin := t.TempDir()
	fixture.log = filepath.Join(bin, "gates")
	stand := "#!/bin/sh\n[ \"$1\" = vet ] && exit 0\n" +
		"echo \"start $(basename \"$PWD\") $(ls | tr '\\n' ' ')\" >> '" + fixture.log + "'\nsleep 1\n" +
		"echo \"end $(basename \"$PWD\")\" >> '" + fixture.log + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}
	home := privatePool(t)
	fixture.environment = append(outsideThePool(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+home, "XDG_CACHE_HOME="+filepath.Join(home, "cache"), tokensVariable+"=2",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")

	fixture.git(fixture.repository, "init", "-q", "-b", "main")
	fixture.commit(fixture.repository, "base.txt")
	for _, name := range []string{"a", "b"} {
		fixture.git(fixture.repository, "worktree", "add", "-q", "-b", name, fixture.worktree(name))
		fixture.commit(fixture.worktree(name), name+".txt")
	}
	return fixture
}

func (fixture *landFixture) worktree(name string) string {
	return filepath.Join(filepath.Dir(fixture.repository), filepath.Base(fixture.repository)+"-"+name)
}

func (fixture *landFixture) git(directory string, arguments ...string) string {
	fixture.t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir, command.Env = directory, fixture.environment
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %v in %s: %v\n%s", arguments, directory, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *landFixture) commit(directory string, file string) {
	fixture.t.Helper()
	if err := os.WriteFile(filepath.Join(directory, file), []byte(file+"\n"), 0o644); err != nil {
		fixture.t.Fatal(err)
	}
	fixture.git(directory, "add", file)
	fixture.git(directory, "commit", "-q", "-m", "add "+file)
}

func (fixture *landFixture) land(name string) *exec.Cmd {
	fixture.t.Helper()
	command := exec.Command(fixture.wrapper, "land")
	command.Dir, command.Env = fixture.worktree(name), fixture.environment
	if err := command.Start(); err != nil {
		fixture.t.Fatal(err)
	}
	return command
}

func (fixture *landFixture) waitForGate(line string) {
	fixture.t.Helper()
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
		contents, _ := os.ReadFile(fixture.log)
		if strings.Contains(string(contents), line) {
			return
		}
		if time.Now().After(deadline) {
			fixture.t.Fatalf("waited a minute for %q in the gate log:\n%s", line, contents)
		}
	}
}

func (fixture *landFixture) gates() []string {
	contents, _ := os.ReadFile(fixture.log)
	return strings.Split(strings.TrimSpace(string(contents)), "\n")
}

// Two branches landing at once both land, in the order they asked, and their gates never overlap: the second
// waits for the first to reach main, then merges main and gates its work with the first's in the tree
// (#fz6xejy). Before, the second gated a main the first was about to move, and would gate again or land
// what nobody had tested together.
func TestTwoLandingsBothLandInOrderWithoutOverlap(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	first := fixture.land("a")
	fixture.waitForGate("start " + filepath.Base(fixture.worktree("a")))
	second := fixture.land("b")
	for name, command := range map[string]*exec.Cmd{"a": first, "b": second} {
		if err := command.Wait(); err != nil {
			t.Errorf("landing %s failed: %v", name, err)
		}
	}

	a, b := filepath.Base(fixture.worktree("a")), filepath.Base(fixture.worktree("b"))
	gates := fixture.gates()
	if len(gates) != 4 || !strings.HasPrefix(gates[0], "start "+a) || gates[1] != "end "+a ||
		!strings.HasPrefix(gates[2], "start "+b) || gates[3] != "end "+b {
		t.Fatalf("the gates ran %q, want a's start and end, then b's", gates)
	}
	if !strings.Contains(gates[2], "a.txt") {
		t.Errorf("b's gate tested %q, without a's landed work merged in", gates[2])
	}
	files := fixture.git(fixture.repository, "ls-files")
	if !strings.Contains(files, "a.txt") || !strings.Contains(files, "b.txt") {
		t.Errorf("main holds %q, want both landings", files)
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, "b.txt")); err != nil {
		t.Error("the checkout of main was not moved with it")
	}
}

// A main moved outside land during the gate is not landed over: git refuses the fast-forward, and land merges
// the moved main and gates again before it lands.
func TestALandingAfterMainMovedOutsideLandGatesAgain(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	landing := fixture.land("a")
	fixture.waitForGate("start " + filepath.Base(fixture.worktree("a")))
	fixture.commit(fixture.repository, "outside.txt")
	if err := landing.Wait(); err != nil {
		t.Fatalf("the landing failed: %v", err)
	}

	gates := fixture.gates()
	if len(gates) != 4 || !strings.Contains(gates[2], "outside.txt") {
		t.Fatalf("the gates ran %q, want a second gate that holds the commit made outside land", gates)
	}
	if files := fixture.git(fixture.repository, "ls-files"); !strings.Contains(files, "a.txt") || !strings.Contains(files, "outside.txt") {
		t.Errorf("main holds %q, want the landing and the commit made outside it", files)
	}
}

// Land refuses work that is not committed, and a merge that conflicts is undone and named.
func TestLandRefusesUncommittedWorkAndUndoesAConflict(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	a := fixture.worktree("a")
	if err := os.WriteFile(filepath.Join(a, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refused := exec.Command(fixture.wrapper, "land")
	refused.Dir, refused.Env = a, fixture.environment
	if output, err := refused.CombinedOutput(); err == nil || !strings.Contains(string(output), "not committed") {
		t.Errorf("land of uncommitted work was not refused (%v):\n%s", err, output)
	}
	fixture.git(a, "commit", "-q", "-am", "change a.txt")

	// Main changes the same file, so the merge conflicts.
	if err := os.WriteFile(filepath.Join(fixture.repository, "a.txt"), []byte("main's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.git(fixture.repository, "add", "a.txt")
	fixture.git(fixture.repository, "commit", "-q", "-m", "main's a.txt")
	before := fixture.git(a, "rev-parse", "HEAD")
	conflicting := exec.Command(fixture.wrapper, "land")
	conflicting.Dir, conflicting.Env = a, fixture.environment
	output, err := conflicting.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "conflicts with this work in") || !strings.Contains(string(output), "a.txt") {
		t.Errorf("a conflicting land did not stop naming the file (%v):\n%s", err, output)
	}
	if after := fixture.git(a, "rev-parse", "HEAD"); after != before || fixture.git(a, "status", "--porcelain", "--untracked-files=no") != "" {
		t.Error("the conflicting merge was not undone")
	}
}
