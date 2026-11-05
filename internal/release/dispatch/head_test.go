package dispatch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// readHead answers what `git rev-parse HEAD` answers, without git. Each case builds a real repository
// in one of the layouts a checkout takes and compares the two, because a wrong commit here builds and
// runs the wrong rules while looking like the right ones.
func TestReadHeadAgreesWithGitInEveryLayout(test *testing.T) {
	test.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		test.Skip("git is needed to build the fixtures and to compare against")
	}
	git := func(directory string, arguments ...string) string {
		test.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = directory
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
		output, err := command.CombinedOutput()
		if err != nil {
			test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	commit := func(directory string, message string) {
		test.Helper()
		writeFile(test, filepath.Join(directory, "file.txt"), message+"\n")
		git(directory, "add", "file.txt")
		git(directory, "commit", "--quiet", "-m", message)
	}
	agree := func(name string, directory string) {
		test.Helper()
		want := git(directory, "rev-parse", "HEAD")
		got, err := readHead(directory)
		if err != nil {
			test.Fatalf("%s: %v", name, err)
		}
		if got != want {
			test.Fatalf("%s: read %s, git says %s", name, got, want)
		}
	}

	repository := test.TempDir()
	git(repository, "init", "--quiet", "--initial-branch=main")
	commit(repository, "first")
	agree("a branch as a loose ref", repository)

	commit(repository, "second")
	git(repository, "pack-refs", "--all")
	if _, err := os.Stat(filepath.Join(repository, ".git", "refs", "heads", "main")); !os.IsNotExist(err) {
		test.Fatalf("the fixture meant to pack its refs still has a loose one: %v", err)
	}
	agree("a branch only in packed-refs", repository)

	// Packed, then moved on: the loose ref is newer than the packed line and has to win.
	commit(repository, "third")
	agree("a loose ref beside a stale packed one", repository)

	worktree := filepath.Join(test.TempDir(), "worktree")
	git(repository, "worktree", "add", "--quiet", "-b", "side", worktree)
	commit(worktree, "on the side")
	agree("a worktree's own branch", worktree)
	agree("the main checkout beside a worktree", repository)

	git(repository, "checkout", "--quiet", "--detach", "HEAD~1")
	agree("a detached HEAD", repository)

	// A submodule's `.git` is a file pointing into the parent's modules directory.
	parent := test.TempDir()
	git(parent, "init", "--quiet", "--initial-branch=main")
	commit(parent, "parent")
	git(parent, "-c", "protocol.file.allow=always", "submodule", "--quiet", "add", repository, "vendored")
	agree("a submodule's gitlink", filepath.Join(parent, "vendored"))

	if _, err := readHead(test.TempDir()); err == nil {
		test.Fatal("a directory that is no checkout produced a commit")
	}
}

func TestReadHeadRefusesWhatIsNotACommit(test *testing.T) {
	test.Parallel()

	repository := test.TempDir()
	writeFile(test, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeFile(test, filepath.Join(repository, ".git", "refs", "heads", "main"), "abc123\n")
	if _, err := readHead(repository); err == nil || !strings.Contains(err.Error(), "not a commit") {
		test.Fatalf("an abbreviated object name was accepted: %v", err)
	}

	writeFile(test, filepath.Join(repository, ".git", "HEAD"), "ref: ../../outside\n")
	if _, err := readHead(repository); err == nil {
		test.Fatal("a ref name walking out of the git directory was followed")
	}

	writeFile(test, filepath.Join(repository, ".git", "HEAD"), "ref: refs/heads/.invalid\n")
	writeFile(test, filepath.Join(repository, ".git", "config"), "[extensions]\n\trefStorage = reftable\n")
	if _, err := readHead(repository); err == nil || !strings.Contains(err.Error(), "reftable") {
		test.Fatalf("a reftable repository was read as though its files held the refs: %v", err)
	}
}

// After the first run at a commit, the Swift engine's inputs are read from what that run kept, and git
// is not asked again: the module directory here stops being a repository at all, and the answer holds.
func TestSwiftEngineInputsAreAskedOncePerCommit(test *testing.T) {
	test.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		test.Skip("git is needed for the first answer")
	}
	module := test.TempDir()
	run := func(arguments ...string) string {
		test.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = module
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
			"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
		output, err := command.CombinedOutput()
		if err != nil {
			test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--quiet")
	writeFile(test, filepath.Join(module, "swift", "Package.swift"), "// package\n")
	writeFile(test, filepath.Join(module, "swift", "Package.resolved"), "{}\n")
	writeFile(test, filepath.Join(module, "swift", "Sources", "A.swift"), "let a = 1\n")
	run("add", ".")
	run("commit", "--quiet", "-m", "engine")
	commit := run("rev-parse", "HEAD")

	paths := Paths{ModuleDirectory: module, CacheDirectory: test.TempDir()}
	first, err := swiftEngineInputObjects(paths, commit)
	if err != nil {
		test.Fatal(err)
	}

	if err := os.Rename(filepath.Join(module, ".git"), filepath.Join(module, "not-git")); err != nil {
		test.Fatal(err)
	}
	second, err := swiftEngineInputObjects(paths, commit)
	if err != nil {
		test.Fatalf("the second run asked git again: %v", err)
	}
	if strings.Join(first, " ") != strings.Join(second, " ") {
		test.Fatalf("the kept answer %v differs from git's %v", second, first)
	}

	// A kept answer that is not whole is asked again, which here fails loudly rather than being trusted.
	entries, err := os.ReadDir(filepath.Join(paths.CacheDirectory, "swift-inputs"))
	if err != nil || len(entries) != 1 {
		test.Fatalf("expected one kept answer, found %v (err %v)", entries, err)
	}
	writeFile(test, filepath.Join(paths.CacheDirectory, "swift-inputs", entries[0].Name()), first[0]+"\n")
	if _, err := swiftEngineInputObjects(paths, commit); err == nil {
		test.Fatal("a kept answer with a missing object was trusted")
	}
}

// The committed build names the compiler's repository from the snapshot's committed `.gitmodules`: the
// entry whose path is the compiler's directory, whatever its name or position, in either url shape. A
// snapshot has no `.git`, so a version that asked the submodule's origin instead would read nothing here
// and fail every case that expects a name.
func TestTheCompilerRepositoryIsReadFromTheCommittedGitmodules(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name       string
		gitmodules string
		want       string
	}{
		{"the fork, after another submodule, over ssh",
			"[submodule \"libraries/other\"]\n\tpath = libraries/other\n\turl = https://github.com/someone/other.git\n" +
				"[submodule \"compiler\"]\n\tpath = TypeScript\n\turl = git@github.com:system-inc/TypeScript.git\n",
			"system-inc/TypeScript"},
		{"over https, quoted", "[submodule \"TypeScript\"]\n\tpath = \"TypeScript\"\n\turl = \"https://github.com/system-inc/TypeScript.git\"\n\tbranch = adamic\n",
			"system-inc/TypeScript"},
		{"no entry for the compiler", "[submodule \"other\"]\n\tpath = other\n\turl = https://github.com/someone/other.git\n", ""},
		{"a url that names no repository", "[submodule \"TypeScript\"]\n\tpath = TypeScript\n\turl = https://github.com/\n", ""},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()

			snapshot := test.TempDir()
			writeFile(test, filepath.Join(snapshot, ".gitmodules"), testCase.gitmodules)
			if got := committedCompilerUpstream(snapshot); got != testCase.want {
				test.Fatalf("named %q, want %q", got, testCase.want)
			}
		})
	}
	if got := committedCompilerUpstream(test.TempDir()); got != "" {
		test.Fatalf("a snapshot with no .gitmodules named %q", got)
	}
}
