//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// landFixture is a repository with main checked out and two worktrees, a and b, each one commit ahead on a
// file of its own, and a cohere-dev whose go stands in for the gate: it logs when each worktree's test run
// starts and ends, and which files the tree it tests holds.
type landFixture struct {
	test        *testing.T
	wrapper     string
	repository  string
	log         string
	environment []string
}

func newLandFixture(test *testing.T) *landFixture {
	test.Helper()
	fixture := &landFixture{
		test:       test,
		wrapper:    filepath.Join(test.TempDir(), "cohere-dev"),
		repository: test.TempDir(),
	}
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command(
		"go",
		"build",
		"-o",
		fixture.wrapper,
		".",
	).CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	bin := test.TempDir()
	fixture.log = filepath.Join(bin, "gates")
	stand := "#!/bin/sh\n[ \"$1\" = vet ] && exit 0\n" +
		"echo \"start $(basename \"$PWD\") $(ls | tr '\\n' ' ')tree=$(git rev-parse HEAD^{tree}) " +
		"handed=$" + landWorktreeVariable + " " +
		"flags=$GOFLAGS\" >> '" +
		fixture.log + "'\nsleep 1\n" +
		"echo \"end $(basename \"$PWD\")\" >> '" + fixture.log + "'\n"
	if writeError := os.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	home := privatePool(test)
	fixture.environment = append(
		outsideThePool(os.Environ()),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		homeVariable+"="+home,
		tokensVariable+"=2",
		// A fixture's landing never reaches the real Home: its commits are no one's to push to the feed. A test that
		// means Home stands in for her with a later entry, which wins.
		homeGateVariable+"=off",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)

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
	fixture.test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir, command.Env = directory, fixture.environment
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		fixture.test.Fatalf("git %v in %s: %v\n%s", arguments, directory, outputError, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *landFixture) commit(directory string, file string) {
	fixture.test.Helper()
	if writeError := os.WriteFile(filepath.Join(directory, file), []byte(file+"\n"), 0o644); writeError != nil {
		fixture.test.Fatal(writeError)
	}
	fixture.git(directory, "add", file)
	fixture.git(directory, "commit", "-q", "-m", fixtureSubject("add "+file))
}

// fixtureTask is the task every fixture commit names, so a landing finds one.
const fixtureTask = "tskfix1"

// fixtureSubject is a subject the house writes, `area: what changed (#task)`, its area base.txt, the fixture's
// one top-level file.
func fixtureSubject(what string) string {
	return "base: " + what + " (#" + fixtureTask + ")"
}

func (fixture *landFixture) land(name string) *exec.Cmd {
	fixture.test.Helper()
	command := exec.Command(fixture.wrapper, "land")
	command.Dir, command.Env = fixture.worktree(name), fixture.environment
	if startError := command.Start(); startError != nil {
		fixture.test.Fatal(startError)
	}
	return command
}

func (fixture *landFixture) waitForGate(line string) {
	fixture.test.Helper()
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
		contents := readMaybeMissing(fixture.test, fixture.log)
		if strings.Contains(string(contents), line) {
			return
		}
		if time.Now().After(deadline) {
			fixture.test.Fatalf("waited a minute for %q in the gate log:\n%s", line, contents)
		}
	}
}

func (fixture *landFixture) gates() []string {
	contents := readMaybeMissing(fixture.test, fixture.log)
	return strings.Split(strings.TrimSpace(string(contents)), "\n")
}

// Two branches landing at once both land, in the order they asked, and their gates never overlap: the second
// waits for the first to reach main, then merges main and gates its work with the first's in the tree
// (#fz6xejy). Before, the second gated a main the first was about to move, and would gate again or land
// what nobody had tested together.
func TestTwoLandingsBothLandInOrderWithoutOverlap(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	first := fixture.land("a")
	fixture.waitForGate("start " + filepath.Base(fixture.worktree("a")))
	second := fixture.land("b")
	for name, command := range map[string]*exec.Cmd{"a": first, "b": second} {
		if waitError := command.Wait(); waitError != nil {
			test.Errorf("landing %s failed: %v", name, waitError)
		}
	}

	worktreeA, worktreeB := filepath.Base(fixture.worktree("a")), filepath.Base(fixture.worktree("b"))
	gates := fixture.gates()
	if len(gates) != 4 || !strings.HasPrefix(gates[0], "start "+worktreeA) || gates[1] != "end "+worktreeA ||
		!strings.HasPrefix(gates[2], "start "+worktreeB) || gates[3] != "end "+worktreeB {
		test.Fatalf("the gates ran %q, want a's start and end, then b's", gates)
	}
	if !strings.Contains(gates[2], "a.txt") {
		test.Errorf("b's gate tested %q, without a's landed work merged in", gates[2])
	}
	files := fixture.git(fixture.repository, "ls-files")
	if !strings.Contains(files, "a.txt") || !strings.Contains(files, "b.txt") {
		test.Errorf("main holds %q, want both landings", files)
	}
	if _, statError := os.Stat(filepath.Join(fixture.repository, "b.txt")); statError != nil {
		test.Error("the checkout of main was not moved with it")
	}
}

// A main moved outside land during the gate is not landed over: git refuses the fast-forward, and land merges
// the moved main and gates again before it lands.
func TestALandingAfterMainMovedOutsideLandGatesAgain(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	landing := fixture.land("a")
	fixture.waitForGate("start " + filepath.Base(fixture.worktree("a")))
	fixture.commit(fixture.repository, "outside.txt")
	if waitError := landing.Wait(); waitError != nil {
		test.Fatalf("the landing failed: %v", waitError)
	}

	gates := fixture.gates()
	if len(gates) != 4 || !strings.Contains(gates[2], "outside.txt") {
		test.Fatalf("the gates ran %q, want a second gate that holds the commit made outside land", gates)
	}
	// The landing joins the moved main: its first parent is the commit made outside land.
	if parent, outside := fixture.git(fixture.repository, "rev-parse", "main^1"),
		fixture.git(fixture.repository, "log", "-1", "--format=%H", "--", "outside.txt"); parent != outside {
		test.Errorf("the landing's first parent is %s, want the commit made outside land, %s", parent, outside)
	}
	if files := fixture.git(
		fixture.repository,
		"ls-files",
	); !strings.Contains(files, "a.txt") || !strings.Contains(files, "outside.txt") {
		test.Errorf("main holds %q, want the landing and the commit made outside it", files)
	}
}

// Land refuses work that is not committed, and a merge that conflicts is undone and named.
func TestLandRefusesUncommittedWorkAndUndoesAConflict(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	worktreeA := fixture.worktree("a")
	if writeError := os.WriteFile(filepath.Join(worktreeA, "a.txt"), []byte("changed\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	refused := exec.Command(fixture.wrapper, "land")
	refused.Dir, refused.Env = worktreeA, fixture.environment
	if output, outputError := refused.CombinedOutput(); outputError == nil ||
		!strings.Contains(string(output), "not committed") {
		test.Errorf("land of uncommitted work was not refused (%v):\n%s", outputError, output)
	}
	fixture.git(worktreeA, "commit", "-q", "-am", fixtureSubject("change a.txt"))

	// Main changes the same file, so the merge conflicts.
	if writeError := os.WriteFile(
		filepath.Join(fixture.repository, "a.txt"),
		[]byte("main's\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.git(fixture.repository, "add", "a.txt")
	fixture.git(fixture.repository, "commit", "-q", "-m", "main's a.txt")
	before := fixture.git(worktreeA, "rev-parse", "HEAD")
	conflicting := exec.Command(fixture.wrapper, "land", "--message", "a.txt changed")
	conflicting.Dir, conflicting.Env = worktreeA, fixture.environment
	output, outputError := conflicting.CombinedOutput()
	if outputError == nil ||
		!strings.Contains(string(output), "conflicts with this work in") ||
		!strings.Contains(string(output), "a.txt") {
		test.Errorf("a conflicting land did not stop naming the file (%v):\n%s", outputError, output)
	}
	if after := fixture.git(
		worktreeA,
		"rev-parse",
		"HEAD",
	); after != before || fixture.git(worktreeA, "status", "--porcelain", "--untracked-files=no") != "" {
		test.Error("the conflicting merge was not undone")
	}
}

// A worktree whose submodule is a symlink to the main checkout's, as most of the house's are made, lands
// when that checkout is at the pin, and is refused, naming both commits, when it is not. git status refuses
// such a worktree outright, which land used to read as uncommitted changes with none named (#fz6xejy).
func TestLandChecksASymlinkedSubmoduleAgainstItsPin(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	library := test.TempDir()
	fixture.git(library, "init", "-q", "-b", "main")
	fixture.commit(library, "first.txt")
	pinned := fixture.git(library, "rev-parse", "HEAD")
	fixture.commit(library, "second.txt")
	moved := fixture.git(library, "rev-parse", "HEAD")
	fixture.git(library, "checkout", "-q", "--detach", pinned)

	fixture.git(fixture.repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "pin library")
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	// The worktree's submodule, a symlink to the main checkout's, at the pin.
	if removeError := os.RemoveAll(filepath.Join(worktreeA, "library")); removeError != nil {
		test.Fatal(removeError)
	}
	if symlinkError := os.Symlink(
		filepath.Join(fixture.repository, "library"),
		filepath.Join(worktreeA, "library"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}

	// Moved off the pin, it is refused, both commits named.
	fixture.git(filepath.Join(fixture.repository, "library"), "checkout", "-q", "--detach", moved)
	refused := exec.Command(fixture.wrapper, "land")
	refused.Dir, refused.Env = worktreeA, fixture.environment
	output, outputError := refused.CombinedOutput()
	if outputError == nil ||
		!strings.Contains(string(output), "pins "+pinned[:8]) ||
		!strings.Contains(string(output), "library is at") {
		test.Errorf("a submodule off its pin was not refused by name (%v):\n%s", outputError, output)
	}

	// Back on the pin, it lands.
	fixture.git(filepath.Join(fixture.repository, "library"), "checkout", "-q", "--detach", pinned)
	landing := exec.Command(fixture.wrapper, "land")
	landing.Dir, landing.Env = worktreeA, fixture.environment
	if output, outputError := landing.CombinedOutput(); outputError != nil ||
		!strings.Contains(string(output), "landed") {
		test.Errorf("a worktree with a symlinked submodule at its pin did not land (%v):\n%s", outputError, output)
	}
	if files := fixture.git(fixture.repository, "ls-files"); !strings.Contains(files, "a.txt") {
		test.Errorf("main holds %q after the landing", files)
	}
}

// gitlinkRepository is a repository with one commit, and the git that runs in it, with no one's settings.
func gitlinkRepository(test *testing.T) (string, func(directory string, arguments ...string) string) {
	test.Helper()
	environment := append(
		os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	git := func(directory string, arguments ...string) string {
		test.Helper()
		command := exec.Command("git", arguments...)
		command.Dir, command.Env = directory, environment
		output, outputError := command.CombinedOutput()
		if outputError != nil {
			test.Fatalf("git %v in %s: %v\n%s", arguments, directory, outputError, output)
		}
		return strings.TrimSpace(string(output))
	}
	repository := test.TempDir()
	git(repository, "init", "-q", "-b", "main")
	if writeError := os.WriteFile(filepath.Join(repository, "base.txt"), []byte("base\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	git(repository, "add", "base.txt")
	git(repository, "commit", "-q", "-m", "add base.txt")
	return repository, git
}

// A commit that records a submodule as its gitlink passes, and one that replaces the gitlink with a symlink to
// another checkout's, as a merge did with TypeScript on 2026-11-05 (ee1660af), is refused naming the path, the
// mode it found and the fix, whose command, run as given, makes a commit that passes again. The symlink is
// planted the way it was then: the submodule's directory replaced on disk, and `git add` of the path.
func TestTheGateRefusesACommitThatRecordsASubmoduleAsASymlink(test *testing.T) {
	test.Parallel()
	repository, git := gitlinkRepository(test)
	library := test.TempDir()
	git(library, "init", "-q", "-b", "main")
	git(library, "commit", "-q", "--allow-empty", "-m", "the library")
	pinned := git(library, "rev-parse", "HEAD")
	git(repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	git(repository, "commit", "-q", "-m", "pin library")
	if checkGitlinksError := checkGitlinks(repository, "HEAD"); checkGitlinksError != nil {
		test.Fatalf("a commit that records its submodule as a gitlink was refused: %v", checkGitlinksError)
	}

	if removeError := os.RemoveAll(filepath.Join(repository, "library")); removeError != nil {
		test.Fatal(removeError)
	}
	if symlinkError := os.Symlink(library, filepath.Join(repository, "library")); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	git(repository, "add", "library")
	git(repository, "commit", "-q", "-m", "the symlink in the submodule's place")
	if entry := git(repository, "ls-tree", "HEAD", "--", "library"); !strings.HasPrefix(entry, "120000 ") {
		test.Fatalf("the planted commit records %q, want the symlink, mode 120000", entry)
	}
	refusal := checkGitlinks(repository, "HEAD")
	fix := "git update-index --cacheinfo 160000," + pinned + ",library"
	if refusal == nil ||
		!strings.Contains(refusal.Error(), "records library, which .gitmodules names a submodule, as mode 120000") ||
		!strings.Contains(refusal.Error(), fix) {
		test.Fatalf(
			"a commit that records its submodule as a symlink was not refused by path, mode and fix: %v",
			refusal,
		)
	}

	git(repository, strings.Fields(fix)[1:]...)
	git(repository, "commit", "-q", "-m", "the gitlink restored")
	if checkGitlinksError := checkGitlinks(repository, "HEAD"); checkGitlinksError != nil {
		test.Errorf("the commit the refusal's fix made was refused: %v", checkGitlinksError)
	}
}

// The gitlink check runs before any test, in the two gates that read a tree whose symlink resolves: a whole-module
// run from the root, as the Square runs on every merge, and a landing whose merge of main brings the symlink in.
func TestAWholeModuleRunAndALandingRefuseASubmoduleRecordedAsASymlink(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	library := fixture.pinLibrary()
	if removeError := os.RemoveAll(library); removeError != nil {
		test.Fatal(removeError)
	}
	if symlinkError := os.Symlink(test.TempDir(), library); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	fixture.git(fixture.repository, "add", "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "the symlink in the submodule's place")

	whole := exec.Command(fixture.wrapper, "test", "./...")
	whole.Dir, whole.Env = fixture.repository, fixture.environment
	if output, outputError := whole.CombinedOutput(); outputError == nil ||
		!strings.Contains(string(output), "records library, which .gitmodules names a submodule, as mode 120000") {
		test.Errorf(
			"a whole-module run of a symlinked submodule was not refused by name (%v):\n%s",
			outputError,
			output,
		)
	}
	landing := exec.Command(fixture.wrapper, "land")
	landing.Dir, landing.Env = fixture.worktree("a"), fixture.environment
	if output, outputError := landing.CombinedOutput(); outputError == nil ||
		!strings.Contains(string(output), "after merging main at") ||
		!strings.Contains(string(output), "as mode 120000") {
		test.Errorf(
			"a landing that merged in a symlinked submodule was not refused by name (%v):\n%s",
			outputError,
			output,
		)
	}
	if contents := readMaybeMissing(test, fixture.log); len(contents) > 0 {
		test.Errorf("go ran before the refusal:\n%s", contents)
	}
}

// A repository with no .gitmodules has no submodule to check, and passes.
func TestTheGitlinkCheckPassesARepositoryWithNoSubmodules(test *testing.T) {
	test.Parallel()
	repository, _ := gitlinkRepository(test)
	if checkGitlinksError := checkGitlinks(repository, "HEAD"); checkGitlinksError != nil {
		test.Errorf("a repository with no .gitmodules was refused: %v", checkGitlinksError)
	}
}

// The gate runs the same for every landing, whatever the caller's shell sets: `GOFLAGS` carries only the pool's
// -p and -buildvcs=false. A caller's -trimpath once failed a land gate on tests that find their fixtures by
// their source path.
func TestTheLandGateIgnoresTheCallersGoFlags(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	fixture.environment = append(fixture.environment, "GOFLAGS=-trimpath -buildvcs=false -tags=extra")
	if waitError := fixture.land("a").Wait(); waitError != nil {
		test.Fatalf("the landing failed: %v", waitError)
	}
	gates := fixture.gates()
	if len(gates) != 2 || !strings.HasSuffix(gates[0], "flags=-p=2 -buildvcs=false -trimpath") {
		test.Errorf("the gate ran with %q, want GOFLAGS of the pool's -p, -buildvcs=false and -trimpath alone", gates)
	}
}

// The gate builds Go in a worktree whose submodule is a symlink, as most of the house's are made. git status
// refuses that tree, so a go build stamping VCS fails "error obtaining VCS status", and every test that
// builds a binary failed: cache's land at 04:15 on 2026-10-06, after a full gate. This runs the real go, not
// the fixture's stand-in, which is how the symlinked-submodule landing test above missed it.
func TestTheGateBuildsGoInATreeWhoseSubmoduleIsASymlink(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	library := test.TempDir()
	fixture.git(library, "init", "-q", "-b", "main")
	fixture.commit(library, "first.txt")
	for file, contents := range map[string]string{
		"go.mod":  "module example.com/stamp\n\ngo 1.21\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if writeError := os.WriteFile(
			filepath.Join(fixture.repository, file),
			[]byte(contents),
			0o644,
		); writeError != nil {
			test.Fatal(writeError)
		}
	}
	fixture.git(fixture.repository, "add", "go.mod", "main.go")
	fixture.git(fixture.repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "a module, and library pinned")
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	if removeError := os.RemoveAll(filepath.Join(worktreeA, "library")); removeError != nil {
		test.Fatal(removeError)
	}
	if symlinkError := os.Symlink(
		filepath.Join(fixture.repository, "library"),
		filepath.Join(worktreeA, "library"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}

	// The real go, with whatever flags the caller's shell had, through the gate's environment.
	caller := []string{"GOFLAGS=-trimpath -p=2", "GOWORK=off"}
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "GOFLAGS=") && !strings.HasPrefix(variable, "GOWORK=") {
			caller = append(caller, variable)
		}
	}
	build := exec.Command("go", "build", "-o", filepath.Join(test.TempDir(), "stamp"), ".")
	build.Dir, build.Env = build_inputs.OutsideModule(test, worktreeA), gateEnvironment(caller)
	if output, outputError := build.CombinedOutput(); outputError != nil {
		test.Errorf(
			"the gate's go build failed in a worktree with a symlinked submodule (%v):\n%s",
			outputError,
			output,
		)
	}
}

// The gate covers every module go.work names, not only the root's ./..., and says which (#ejcnkja): a test
// failing in a nested module refuses the landing, and the same landing lands once it passes. A module
// inside a submodule is not gated, by rule: this one's test always fails, and neither landing runs it.
// This runs the real go, not the fixture's stand-in, since what's proven is which packages go is asked to
// test.
func TestTheGateCoversEveryWorkspaceModuleButASubmodules(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	// The real go: the stand-in's directory comes off the front of PATH.
	fixture.environment = append(fixture.environment, "PATH="+os.Getenv("PATH"))
	write := func(directory string, files map[string]string) {
		test.Helper()
		for name, contents := range files {
			path := filepath.Join(directory, name)
			if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}
	passing := "package inner\n\nimport \"testing\"\n\nfunc TestInner(t *testing.T) {}\n"
	failing := "package inner\n\nimport \"testing\"\n\nfunc TestInner(t *testing.T) { t.Fatal(\"planted in the nested module\") }\n"

	library := test.TempDir()
	fixture.git(library, "init", "-q", "-b", "main")
	write(library, map[string]string{
		"mod/go.mod":          "module example.com/library\n\ngo 1.21\n",
		"mod/library_test.go": "package library\n\nimport \"testing\"\n\nfunc TestLibrary(t *testing.T) { t.Fatal(\"a submodule's test ran\") }\n",
	})
	fixture.git(library, "add", ".")
	fixture.git(library, "commit", "-q", "-m", "a module that fails")
	write(fixture.repository, map[string]string{
		"go.mod":              "module example.com/root\n\ngo 1.21\n",
		"root.go":             "package root\n",
		"go.work":             "go 1.21\n\nuse (\n\t.\n\t./inner\n\t./library/mod\n)\n",
		"inner/go.mod":        "module example.com/root/inner\n\ngo 1.21\n",
		"inner/inner_test.go": passing,
	})
	fixture.git(fixture.repository, "add", "go.mod", "root.go", "go.work", "inner")
	fixture.git(fixture.repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "a workspace of three modules, one in a submodule")
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	if removeError := os.RemoveAll(filepath.Join(worktreeA, "library")); removeError != nil {
		test.Fatal(removeError)
	}
	if symlinkError := os.Symlink(
		filepath.Join(fixture.repository, "library"),
		filepath.Join(worktreeA, "library"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}

	land := func() (string, error) {
		landing := exec.Command(fixture.wrapper, "land", "--message", "a nested module tested")
		landing.Dir, landing.Env = worktreeA, fixture.environment
		output, outputError := landing.CombinedOutput()
		return string(output), outputError
	}

	// Staged by name: the worktree's submodule is a symlink, and `commit -a` would commit it as one.
	write(worktreeA, map[string]string{"inner/inner_test.go": failing})
	fixture.git(worktreeA, "add", "inner/inner_test.go")
	fixture.git(worktreeA, "commit", "-q", "-m", "inner: plant a failing test (#"+fixtureTask+")")
	output, landError := land()
	if landError == nil || !strings.Contains(output, "planted in the nested module") {
		test.Fatalf("a failing test in a nested module did not refuse the landing (%v):\n%s", landError, output)
	}
	if !strings.Contains(output, "gating 2 modules from go.work: ., inner") ||
		!strings.Contains(output, "not gated, inside a submodule: library/mod") {
		test.Errorf("the gate did not say what it covered:\n%s", output)
	}
	if strings.Contains(output, "a submodule's test ran") {
		test.Errorf("the gate ran a test inside a submodule:\n%s", output)
	}

	write(worktreeA, map[string]string{"inner/inner_test.go": passing})
	fixture.git(worktreeA, "add", "inner/inner_test.go")
	fixture.git(worktreeA, "commit", "-q", "-m", "inner: the nested test passes (#"+fixtureTask+")")
	if output, landError := land(); landError != nil || !strings.Contains(output, "landed") {
		test.Errorf("the landing did not land once the nested test passed (%v):\n%s", landError, output)
	}
	// The landing commit names what the gate tested by module, as the gate's report does.
	if body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main"); !strings.Contains(
		body,
		"go test of 2 modules from go.work (., inner)",
	) {
		test.Errorf("the landing's body does not name the modules gated:\n%s", body)
	}
}

// The landing gate checks the docs data, so a change that leaves an example stale is refused where it's
// made, not at the next release (#h31rcf0). The fixture's generator stands in for cohere's: -check fails
// while docs/data/example.txt is stale. The refusal names the recapture command, and once the example is
// fresh the same landing lands. This runs the real go through the land fixture.
func TestTheGateRefusesStaleDocsData(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	// The real go: the stand-in's directory comes off the front of PATH.
	fixture.environment = append(fixture.environment, "PATH="+os.Getenv("PATH"))
	worktreeA := fixture.worktree("a")
	write := func(files map[string]string) {
		test.Helper()
		for name, contents := range files {
			path := filepath.Join(worktreeA, name)
			if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}
	write(map[string]string{
		"go.mod": "module example.com/root\n\ngo 1.21\n",
		"internal/docs_data/tools/generate/main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
			"func main() {\n\tcontents, _ := os.ReadFile(\"docs/data/example.txt\")\n" +
			"\tif string(contents) != \"fresh\\n\" {\n\t\tfmt.Println(\"stale: docs/data/example.txt\")\n\t\tos.Exit(1)\n\t}\n}\n",
		"docs/data/example.txt": "stale\n",
	})
	fixture.git(worktreeA, "add", "go.mod", "internal", "docs")
	fixture.git(worktreeA, "commit", "-q", "-m", "docs: a rule change whose example went stale (#"+fixtureTask+")")
	land := func() (string, error) {
		landing := exec.Command(fixture.wrapper, "land", "--message", "a rule change and its example")
		landing.Dir, landing.Env = worktreeA, fixture.environment
		output, outputError := landing.CombinedOutput()
		return string(output), outputError
	}
	if output, landError := land(); landError == nil ||
		!strings.Contains(output, "the docs data is stale, so nothing landed") ||
		!strings.Contains(output, docsDataRecapture) ||
		!strings.Contains(output, "stale: docs/data/example.txt") {
		test.Fatalf("a stale example did not refuse the landing, naming the recapture (%v):\n%s", landError, output)
	}
	// A red gate says it failed, and never exits 75, which says a quiet window held the landing (#vq5b7fs).
	output, landError := land()
	var exited *exec.ExitError
	if !strings.Contains(output, "the gate failed, so nothing landed") ||
		strings.Contains(output, "held by the quiet window") || !errors.As(landError, &exited) ||
		exited.ExitCode() == quietRefusedExitCode {
		test.Errorf("a red gate ended with %v, want it to say it failed, and an exit other than %d:\n%s",
			landError, quietRefusedExitCode, output)
	}
	write(map[string]string{"docs/data/example.txt": "fresh\n"})
	fixture.git(worktreeA, "add", "docs/data/example.txt")
	fixture.git(worktreeA, "commit", "-q", "-m", "docs: the example recaptured (#"+fixtureTask+")")
	if output, landError := land(); landError != nil ||
		!strings.Contains(output, "the docs data is current") ||
		!strings.Contains(output, "landed") {
		test.Errorf("the landing did not land once the example was fresh (%v):\n%s", landError, output)
	}
}

// cohere has its docs data generator where the gate looks for it, so moving it fails here rather than
// quietly turning the gate's docs check off.
func TestCohereHasTheDocsDataGeneratorTheGateRuns(test *testing.T) {
	test.Parallel()
	if _, statError := os.Stat(filepath.Join("..", "..", docsDataGenerator, "main.go")); statError != nil {
		test.Fatalf("the landing gate runs %s, and it is not in this module: %v", docsDataGenerator, statError)
	}
}

// treeGated is the tree the stand-in gate logged for a worktree's run, the one it tested.
func (fixture *landFixture) treeGated(name string) string {
	fixture.test.Helper()
	for _, line := range fixture.gates() {
		if !strings.HasPrefix(line, "start "+filepath.Base(fixture.worktree(name))+" ") {
			continue
		}
		_, after, found := strings.Cut(line, " tree=")
		if found {
			return strings.Fields(after)[0]
		}
	}
	fixture.test.Fatalf("no gate of %s logged its tree:\n%q", name, fixture.gates())
	return ""
}

// A landing is one merge commit onto main (#4v9pmef): main's first-parent line gains exactly one commit, whose
// first parent is main as it was, whose second is the branch's head, and whose tree is the tree the gate tested.
// The branch merged main twice on the way, and neither merge reaches main's line, which reads `Land #<task>:`
// and the branch's one commit. Before, main fast-forwarded onto the branch, its merges of main and all.
func TestALandingIsOneMergeCommitOntoMain(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	worktreeA := fixture.worktree("a")
	for _, file := range []string{"first.txt", "second.txt"} {
		fixture.commit(fixture.repository, file)
		fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	}
	before := fixture.git(fixture.repository, "rev-parse", "main")
	head := fixture.git(worktreeA, "rev-parse", "HEAD")
	if waitError := fixture.land("a").Wait(); waitError != nil {
		test.Fatalf("the landing failed: %v", waitError)
	}

	added := strings.Fields(fixture.git(fixture.repository, "rev-list", "--first-parent", before+"..main"))
	if len(added) != 1 {
		test.Fatalf("main's first-parent line gained %d commits, want the one landing: %v", len(added), added)
	}
	landing := added[0]
	if parents := strings.Fields(fixture.git(fixture.repository, "rev-parse", landing+"^@")); len(parents) != 2 ||
		parents[0] != before || parents[1] != head {
		test.Errorf("the landing's parents are %v, want main as it was, %s, then the branch's head, %s",
			parents, before, head)
	}
	tree, gated := fixture.git(fixture.repository, "rev-parse", landing+"^{tree}"), fixture.treeGated("a")
	if tree != gated {
		test.Errorf("the landing's tree is %s, and the gate tested %s", tree, gated)
	}
	if subject := fixture.git(fixture.repository, "log", "-1", "--format=%s", landing); subject !=
		"Land #"+fixtureTask+": base: add a.txt" {
		test.Errorf("the landing's subject is %q", subject)
	}
	body := fixture.git(fixture.repository, "log", "-1", "--format=%b", landing)
	for _, want := range []string{"a at " + head[:8] + ", onto main at " + before[:8], fixtureSubject("add a.txt"),
		"and 2 merges of the branch's own", "Gated by cohere-dev land in",
		": go vet, go test of the module on the Mac, since "} {
		if !strings.Contains(body, want) {
			test.Errorf("the landing's body lacks %q:\n%s", want, body)
		}
	}
	if branch := fixture.git(fixture.repository, "rev-parse", "a"); branch != head {
		test.Errorf("the branch moved from %s to %s, and a landing leaves it where it was", head, branch)
	}
}

// Two landings read as two lines on main's first-parent log, each `Land #<task>: <what landed>`.
func TestTwoLandingsReadAsTwoLandLines(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	before := fixture.git(fixture.repository, "rev-parse", "main")
	for _, name := range []string{"a", "b"} {
		if waitError := fixture.land(name).Wait(); waitError != nil {
			test.Fatalf("landing %s failed: %v", name, waitError)
		}
	}
	log := fixture.git(fixture.repository, "log", "--first-parent", "--format=%s", before+"..main")
	if want := "Land #" + fixtureTask + ": base: add b.txt\nLand #" + fixtureTask + ": base: add a.txt"; log != want {
		test.Errorf("main's first-parent log reads\n%s\nwant\n%s", log, want)
	}
}

// A landing whose commits break the house's subject is refused before it waits for the lock or gates anything,
// naming each commit, its subject and why: over 72 characters, no area, or an area that names no folder or
// top-level file in the tree, with the folder it likely meant. A merge needs no area but is held to the length.
// A branch that names no task, or two, is refused unless one is given, and several commits need a message.
func TestLandRefusesSubjectsTheHouseDoesNotWrite(test *testing.T) {
	test.Parallel()
	for _, refusal := range []struct {
		name      string
		subject   string
		arguments []string
		want      []string
	}{
		{name: "over 72", subject: "base: " + strings.Repeat("word ", 14) + "(#" + fixtureTask + ")",
			want: []string{"86 characters, over 72"}},
		{name: "no area", subject: "add the thing (#" + fixtureTask + ")",
			want: []string{"no `area: ` before what changed"}},
		{name: "an unknown area", subject: "goengine: a thing (#" + fixtureTask + ")",
			want: []string{`"goengine" names no folder or top-level file in the tree`, `("go_engine" is one)`}},
		{name: "a second task", subject: "base: another thing (#tskfix2)", arguments: []string{"--message", "things"},
			want: []string{"name 2 tasks, #" + fixtureTask + ", #tskfix2", "--task"}},
		{name: "a second commit", subject: fixtureSubject("another thing"),
			want: []string{"the branch brings 2 commits", "--message"}},
	} {
		test.Run(refusal.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandFixture(test)
			worktreeA := fixture.worktree("a")
			engine := filepath.Join(worktreeA, "go_engine")
			if createDirectoryError := os.MkdirAll(engine, 0o755); createDirectoryError != nil {
				test.Fatal(createDirectoryError)
			}
			if writeError := os.WriteFile(filepath.Join(engine, "x.txt"), []byte("x\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			fixture.git(worktreeA, "add", "go_engine")
			fixture.git(worktreeA, "commit", "-q", "-m", refusal.subject)
			before := fixture.git(fixture.repository, "rev-parse", "main")
			landing := exec.Command(fixture.wrapper, append([]string{"land"}, refusal.arguments...)...)
			landing.Dir, landing.Env = worktreeA, fixture.environment
			output, outputError := landing.CombinedOutput()
			if outputError == nil {
				test.Fatalf("the landing was not refused:\n%s", output)
			}
			for _, want := range refusal.want {
				if !strings.Contains(string(output), want) {
					test.Errorf("the refusal lacks %q:\n%s", want, output)
				}
			}
			if after := fixture.git(fixture.repository, "rev-parse", "main"); after != before {
				test.Errorf("main moved, %s to %s, on a refused landing", before, after)
			}
			if contents := readMaybeMissing(test, fixture.log); len(contents) > 0 {
				test.Errorf("the gate ran before the refusal:\n%s", contents)
			}
		})
	}

	// The control: the same branch, its subject the house's and its message given, lands.
	fixture := newLandFixture(test)
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
	landing := exec.Command(fixture.wrapper, "land", "--task", "other1", "--message", "a.txt added")
	landing.Dir, landing.Env = worktreeA, fixture.environment
	if output, outputError := landing.CombinedOutput(); outputError != nil {
		test.Errorf("a landing in the house's shape was refused (%v):\n%s", outputError, output)
	}
	subject := fixture.git(fixture.repository, "log", "-1", "--format=%s", "main")
	if subject != "Land #other1: a.txt added" {
		test.Errorf("the landing's subject is %q, want the task and message given", subject)
	}
}

// A worktree whose cohere-dev differs from main's lands through main's land, run from the main checkout against
// the worktree, once, and main gains exactly one `Land #` commit (#8wpk5tp). Before, the worktree's own land
// ran, and one on old code landed the old way: 50f70d03 fast-forwarded main onto its branch. The stand-in for
// `go run ./command/cohere-dev` in the main checkout logs where it ran and what it was handed, then runs this
// land, which must not hand the landing on again.
func TestALandingWhoseLandDiffersFromMainsRunsMains(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	source := filepath.Join(fixture.repository, "command", "cohere-dev")
	if createDirectoryError := os.MkdirAll(source, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	// land_main.go says main's land takes a landing handed to it; without it the worktree's own land runs.
	for _, file := range []string{"land.txt", landMainFile} {
		if writeError := os.WriteFile(filepath.Join(source, file), []byte("main's land\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	fixture.git(fixture.repository, "add", "command")
	fixture.git(fixture.repository, "commit", "-q", "-m", "cohere-dev: main's land (#"+fixtureTask+")")
	worktreeA := fixture.worktree("a")
	fixture.git(worktreeA, "merge", "-q", "--no-edit", "main")
	if writeError := os.WriteFile(
		filepath.Join(worktreeA, "command", "cohere-dev", "land.txt"),
		[]byte("an older land\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.git(worktreeA, "commit", "-q", "-am", "cohere-dev: an older land (#"+fixtureTask+")")

	log := filepath.Join(test.TempDir(), "main-land")
	standIn := filepath.Join(test.TempDir(), "main-land")
	script := "#!/bin/sh\necho \"$PWD $" + landWorktreeVariable + "\" >> '" + log + "'\n" +
		"exec '" + fixture.wrapper + "' \"$@\"\n"
	if writeError := os.WriteFile(standIn, []byte(script), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	before := fixture.git(fixture.repository, "rev-parse", "main")
	landing := exec.Command(fixture.wrapper, "land", "--message", "an older land")
	landing.Dir, landing.Env = worktreeA, append(fixture.environment, mainLandVariable+"="+standIn)
	output, outputError := landing.CombinedOutput()
	if outputError != nil {
		test.Fatalf("the landing failed (%v):\n%s", outputError, output)
	}
	ran := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, log))), "\n")
	resolved := func(directory string) string {
		test.Helper()
		real, evalSymlinksError := filepath.EvalSymlinks(directory)
		if evalSymlinksError != nil {
			test.Fatal(evalSymlinksError)
		}
		return real
	}
	fields := strings.Fields(strings.Join(ran, " "))
	if len(ran) != 1 || len(fields) != 2 || resolved(fields[0]) != resolved(fixture.repository) ||
		resolved(fields[1]) != resolved(worktreeA) {
		test.Errorf("main's land ran %q, want once, in the main checkout, handed %s", ran, worktreeA)
	}
	// Main's land was handed the worktree, and nothing it started was: its gate's commands saw no variable.
	for _, line := range fixture.gates() {
		if strings.HasPrefix(line, "start ") && !strings.Contains(line, " handed= ") {
			test.Errorf("a gate command inherited the handed worktree: %s", line)
		}
	}
	if count := strings.Count(string(output), "differs from main's, so main's land runs"); count != 1 {
		test.Errorf("the landing said %d times that main's land runs, want once:\n%s", count, output)
	}
	added := strings.Fields(fixture.git(fixture.repository, "rev-list", "--first-parent", before+"..main"))
	subject := fixture.git(fixture.repository, "log", "-1", "--format=%s", "main")
	if len(added) != 1 || !strings.HasPrefix(subject, "Land #") {
		test.Errorf("main's first-parent line gained %v, want one `Land #` commit", added)
	}
}

// A main whose land predates handing a landing on reads the worktree from where it runs, so it can't take one,
// and the worktree's own land lands, as the first landing of the handoff had to. The stand-in never runs.
func TestALandingOntoAMainThatCannotTakeItRunsItsOwnLand(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	source := filepath.Join(fixture.repository, "command", "cohere-dev")
	if createDirectoryError := os.MkdirAll(source, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	oldLand := filepath.Join(source, "land.txt")
	if writeError := os.WriteFile(oldLand, []byte("an old land\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.git(fixture.repository, "add", "command")
	fixture.git(fixture.repository, "commit", "-q", "-m", "cohere-dev: an old land (#"+fixtureTask+")")
	log := filepath.Join(test.TempDir(), "main-land")
	standIn := filepath.Join(test.TempDir(), "main-land")
	script := []byte("#!/bin/sh\necho ran >> '" + log + "'\nexit 1\n")
	if writeError := os.WriteFile(standIn, script, 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	landing := exec.Command(fixture.wrapper, "land")
	landing.Dir, landing.Env = fixture.worktree("a"), append(fixture.environment, mainLandVariable+"="+standIn)
	output, outputError := landing.CombinedOutput()
	if outputError != nil || strings.Contains(string(output), "main's land runs") {
		test.Errorf("a landing onto a main that can't take one did not run its own land (%v):\n%s", outputError, output)
	}
	if ran := readMaybeMissing(test, log); len(ran) > 0 {
		test.Errorf("the stand-in for main's land ran:\n%s", ran)
	}
}

// The gate's fifth step refuses a landing unless cohere on the tree reads 0 (#06g3f7x): a finding is refused
// with the finding printed, a run that passes but names a gap is refused naming it, and a clean run lands.
// Before, the gate never ran cohere on cohere, and two landings that each read green brought main 8 findings.
// The stand-in prints what `cohere --no-fix --other-builds --json` would, and keeps the command line it was given,
// which must check every build (#72n9mq0): without `--other-builds` a file only another platform builds is a gap the
// gate would refuse every landing over, never checked.
func TestTheGateRefusesALandingCohereDoesNotReadZeroOn(test *testing.T) {
	test.Parallel()
	clean := `{"kind":"summary","schemaVersion":1,"label":".","verdict":"pass","findings":0,"typeErrors":0,` +
		`"gaps":{"programFiles":0,"crashedFiles":0,"formattingNotChecked":false,"crashes":[]}}`
	for _, gate := range []struct {
		name   string
		output string
		exit   int
		lands  bool
		want   []string
	}{
		{name: "a clean tree", output: clean, lands: true, want: []string{"cohere on the tree reads 0"}},
		{
			name: "one finding",
			output: `{"kind":"finding","path":"/repo/a.go","line":3,"column":7,"severity":"error",` +
				`"rule":"cohere-go/consistency-no-long-line","message":"This line is 121 columns"}` + "\n" +
				`{"kind":"summary","label":".","verdict":"fail","findings":1,"typeErrors":0,"gaps":{}}`,
			exit: 1,
			want: []string{"/repo/a.go:3:7 cohere-go/consistency-no-long-line This line is 121 columns",
				".: verdict fail; 1 findings"},
		},
		{
			name:   "a gap",
			output: `{"kind":"summary","label":"swift","verdict":"pass","findings":0,"typeErrors":0,"gaps":{"crashedFiles":2}}`,
			want:   []string{"swift: gaps: crashedFiles=2"},
		},
		{name: "no summary", output: "", want: []string{"cohere printed no summary"}},
	} {
		test.Run(gate.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandFixture(test)
			records := filepath.Join(test.TempDir(), "records.jsonl")
			if writeError := os.WriteFile(records, []byte(gate.output+"\n"), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
			standIn := filepath.Join(test.TempDir(), "cohere")
			given := filepath.Join(test.TempDir(), "given")
			script := fmt.Sprintf("#!/bin/sh\necho \"$*\" > '%s'\ngrep . '%s'\nexit %d\n", given, records, gate.exit)
			if writeError := os.WriteFile(standIn, []byte(script), 0o755); writeError != nil {
				test.Fatal(writeError)
			}
			before := fixture.git(fixture.repository, "rev-parse", "main")
			landing := exec.Command(fixture.wrapper, "land")
			landing.Dir = fixture.worktree("a")
			landing.Env = append(fixture.environment, gateCohereVariable+"="+standIn)
			output, outputError := landing.CombinedOutput()
			const wantGiven = "--no-fix --other-builds --json"
			if ran := strings.TrimSpace(string(readMaybeMissing(test, given))); ran != wantGiven {
				test.Errorf("the gate ran cohere %q, want %q, which checks every build:\n%s", ran, wantGiven, output)
			}
			for _, want := range gate.want {
				if !strings.Contains(string(output), want) {
					test.Errorf("the landing's output lacks %q:\n%s", want, output)
				}
			}
			after := fixture.git(fixture.repository, "rev-parse", "main")
			if gate.lands {
				if outputError != nil || after == before {
					test.Errorf("a clean tree did not land (%v):\n%s", outputError, output)
				}
				if body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main"); !strings.Contains(
					body,
					"cohere on the tree, 0 findings and no gaps",
				) {
					test.Errorf("the landing's body does not say cohere ran:\n%s", body)
				}
				return
			}
			refused := strings.Contains(string(output), "did not read 0, so nothing landed")
			if outputError == nil || after != before || !refused {
				test.Errorf("the landing was not refused (%v, main %s to %s):\n%s", outputError, before, after, output)
			}
		})
	}
}
