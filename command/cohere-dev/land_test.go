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

func newLandFixture(test *testing.T) *landFixture {
	test.Helper()
	fixture := &landFixture{t: test, wrapper: filepath.Join(test.TempDir(), "cohere-dev"), repository: test.TempDir()}
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
		"echo \"start $(basename \"$PWD\") $(ls | tr '\\n' ' ')flags=$GOFLAGS\" >> '" + fixture.log + "'\nsleep 1\n" +
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
	fixture.t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir, command.Env = directory, fixture.environment
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		fixture.t.Fatalf("git %v in %s: %v\n%s", arguments, directory, outputError, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture *landFixture) commit(directory string, file string) {
	fixture.t.Helper()
	if writeError := os.WriteFile(filepath.Join(directory, file), []byte(file+"\n"), 0o644); writeError != nil {
		fixture.t.Fatal(writeError)
	}
	fixture.git(directory, "add", file)
	fixture.git(directory, "commit", "-q", "-m", "add "+file)
}

func (fixture *landFixture) land(name string) *exec.Cmd {
	fixture.t.Helper()
	command := exec.Command(fixture.wrapper, "land")
	command.Dir, command.Env = fixture.worktree(name), fixture.environment
	if startError := command.Start(); startError != nil {
		fixture.t.Fatal(startError)
	}
	return command
}

func (fixture *landFixture) waitForGate(line string) {
	fixture.t.Helper()
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
		contents := readMaybeMissing(fixture.t, fixture.log)
		if strings.Contains(string(contents), line) {
			return
		}
		if time.Now().After(deadline) {
			fixture.t.Fatalf("waited a minute for %q in the gate log:\n%s", line, contents)
		}
	}
}

func (fixture *landFixture) gates() []string {
	contents := readMaybeMissing(fixture.t, fixture.log)
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
	fixture.git(worktreeA, "commit", "-q", "-am", "change a.txt")

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
	conflicting := exec.Command(fixture.wrapper, "land")
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
	build.Dir, build.Env = worktreeA, gateEnvironment(caller)
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
		landing := exec.Command(fixture.wrapper, "land")
		landing.Dir, landing.Env = worktreeA, fixture.environment
		output, outputError := landing.CombinedOutput()
		return string(output), outputError
	}

	// Staged by name: the worktree's submodule is a symlink, and `commit -a` would commit it as one.
	write(worktreeA, map[string]string{"inner/inner_test.go": failing})
	fixture.git(worktreeA, "add", "inner/inner_test.go")
	fixture.git(worktreeA, "commit", "-q", "-m", "plant a failing test in the nested module")
	output, err := land()
	if err == nil || !strings.Contains(output, "planted in the nested module") {
		test.Fatalf("a failing test in a nested module did not refuse the landing (%v):\n%s", err, output)
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
	fixture.git(worktreeA, "commit", "-q", "-m", "the nested module's test passes")
	if output, err := land(); err != nil || !strings.Contains(output, "landed") {
		test.Errorf("the landing did not land once the nested test passed (%v):\n%s", err, output)
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
		"internal/docsdata/tools/generate/main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\n" +
			"func main() {\n\tcontents, _ := os.ReadFile(\"docs/data/example.txt\")\n" +
			"\tif string(contents) != \"fresh\\n\" {\n\t\tfmt.Println(\"stale: docs/data/example.txt\")\n\t\tos.Exit(1)\n\t}\n}\n",
		"docs/data/example.txt": "stale\n",
	})
	fixture.git(worktreeA, "add", "go.mod", "internal", "docs")
	fixture.git(worktreeA, "commit", "-q", "-m", "a rule change whose example went stale")
	land := func() (string, error) {
		landing := exec.Command(fixture.wrapper, "land")
		landing.Dir, landing.Env = worktreeA, fixture.environment
		output, outputError := landing.CombinedOutput()
		return string(output), outputError
	}
	if output, err := land(); err == nil || !strings.Contains(output, "the docs data is stale, so nothing landed") ||
		!strings.Contains(output, docsDataRecapture) || !strings.Contains(output, "stale: docs/data/example.txt") {
		test.Fatalf("a stale example did not refuse the landing, naming the recapture (%v):\n%s", err, output)
	}
	write(map[string]string{"docs/data/example.txt": "fresh\n"})
	fixture.git(worktreeA, "add", "docs/data/example.txt")
	fixture.git(worktreeA, "commit", "-q", "-m", "the example recaptured")
	if output, err := land(); err != nil ||
		!strings.Contains(output, "the docs data is current") ||
		!strings.Contains(output, "landed") {
		test.Errorf("the landing did not land once the example was fresh (%v):\n%s", err, output)
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
