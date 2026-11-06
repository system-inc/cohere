package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// The inputs record adds what describing the project read to the load's: the ignore files of the nearest repository
// above the root, there or absent from the repository down to the root, and there in a directory the load names; no
// `.git` between the root and the repository; the repository's info/exclude, and its index at a go.work root; the
// settings file; a file of ours nobody could read; and a staticcheck.conf in an analyzed package's directory and every
// one above it. Nothing is said of the ignore files above the repository, which git never reads.
func TestTheInputsRecordNamesWhatDescribingTheProjectRead(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	repository := filepath.Join(base, "repository")
	root := filepath.Join(repository, "project")
	for _, directory := range []string{filepath.Join(repository, ".git"), filepath.Join(root, "sub")} {
		if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
			test.Fatal(makeError)
		}
	}
	for _, file := range []string{
		filepath.Join(repository, ".gitignore"),
		filepath.Join(root, "go.work"),
		filepath.Join(root, "sub", ".gitignore"),
		filepath.Join(repository, ".git", "index"),
		filepath.Join(repository, "staticcheck.conf"),
	} {
		if writeError := os.WriteFile(file, []byte("x\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	unreadable := filepath.Join(root, "sub", "unreadable.go")
	engine := &pipeline{
		request: invocation{root: root},
		described: &describedProject{
			workspace:  project.Workspace{Root: root, FromWorkspace: true},
			unreadable: []contract.UnreadableRecord{{File: unreadable, Error: "permission denied"}},
		},
	}
	loaded := &load.Loaded{
		Packages: []*packages.Package{{Dir: filepath.Join(root, "sub")}},
		Inputs: load.Inputs{
			Files:       []string{filepath.Join(root, "go.work")},
			Directories: []string{root, filepath.Join(root, "sub")},
		},
	}

	record := engine.inputsRecord(loaded)

	for _, want := range []string{
		filepath.Join(repository, ".gitignore"),
		filepath.Join(root, "sub", ".gitignore"),
		filepath.Join(root, "go.work"),
		filepath.Join(repository, ".git", "index"),
		filepath.Join(repository, "staticcheck.conf"),
		unreadable,
	} {
		if !slices.Contains(record.Files, want) {
			test.Errorf("the files do not name %s: %q", want, record.Files)
		}
	}
	for _, want := range []string{
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".git"),
		filepath.Join(root, "go.mod"),
		filepath.Join(root, "CohereSettings.json"),
		filepath.Join(repository, ".git", "info", "exclude"),
		filepath.Join(root, "sub", "staticcheck.conf"),
		filepath.Join(root, "staticcheck.conf"),
		filepath.Join(base, "staticcheck.conf"),
		string(filepath.Separator) + "staticcheck.conf",
	} {
		if !slices.Contains(record.Absent, want) {
			test.Errorf("the absent paths do not name %s: %q", want, record.Absent)
		}
	}
	for _, unwanted := range []string{filepath.Join(repository, ".git"), filepath.Join(base, ".git"),
		filepath.Join(base, ".gitignore")} {
		if slices.Contains(record.Absent, unwanted) || slices.Contains(record.Files, unwanted) {
			test.Errorf("the record names %s, which the census never reads", unwanted)
		}
	}
	if !slices.IsSorted(record.Files) || !slices.IsSorted(record.Absent) {
		test.Errorf("the lists are not sorted: %q, %q", record.Files, record.Absent)
	}
}

// A `.git` that is a file, as a worktree's or a submodule's, points to the git directory whose index git reads,
// relative to the file's directory or absolute; a `.git` directory holds its own.
func TestGitIndexFollowsAGitFileToItsGitDirectory(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	worktree := filepath.Join(base, "worktree")
	if makeError := os.MkdirAll(worktree, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	relativeGitDirectory := filepath.Join(base, "main", ".git", "worktrees", "worktree")
	absoluteGitDirectory := filepath.Join(base, "elsewhere")
	for contents, want := range map[string]string{
		"gitdir: ../main/.git/worktrees/worktree\n": filepath.Join(relativeGitDirectory, "index"),
		"gitdir: " + absoluteGitDirectory + "\n":    filepath.Join(absoluteGitDirectory, "index"),
	} {
		marker := filepath.Join(worktree, ".git")
		if writeError := os.WriteFile(marker, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		if got := gitIndex(marker, false); got != want {
			test.Errorf("a .git file reading %q gives the index %s, want %s", contents, got, want)
		}
	}
	if got, want := gitIndex(filepath.Join(base, ".git"), true), filepath.Join(base, ".git", "index"); got != want {
		test.Errorf("a .git directory gives the index %s, want %s", got, want)
	}
}
