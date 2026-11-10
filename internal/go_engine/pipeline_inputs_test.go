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
// `.git` between the root and the repository; the repository's info/exclude, and its index, which the census reads at
// any root since a tracked path is never ignored; the settings file; a file of ours nobody could read; and a
// staticcheck.conf in an analyzed package's directory and every one above it. Nothing is said of the ignore files above
// the repository, which git never reads.
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
			workspace:  project.Workspace{Root: root},
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
	// The index is named again with what the census read it for, the walk of the root below its repository, so the
	// front door keys it on that rather than on its bytes (#m1daxxk).
	wantTracked := []contract.TrackedIgnoredScope{
		{Index: filepath.Join(repository, ".git", "index"), Root: repository, Scope: "project"},
	}
	if !slices.Equal(record.TrackedIgnored, wantTracked) {
		test.Errorf("the record's trackedIgnored is %+v, want %+v", record.TrackedIgnored, wantTracked)
	}
}

// TestTheInputsRecordNamesNoTrackedIgnoredIndexThatIsNotThere: a repository with no index yet has nothing to key, so
// its index is named absent and never under trackedIgnored.
func TestTheInputsRecordNamesNoTrackedIgnoredIndexThatIsNotThere(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if makeError := os.MkdirAll(filepath.Join(root, ".git"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	engine := &pipeline{
		request:   invocation{root: root},
		described: &describedProject{workspace: project.Workspace{Root: root}},
	}
	record := engine.inputsRecord(&load.Loaded{})
	if !slices.Contains(record.Absent, filepath.Join(root, ".git", "index")) {
		test.Errorf("the absent paths do not name the missing index: %q", record.Absent)
	}
	if len(record.TrackedIgnored) != 0 {
		test.Errorf("a repository with no index names one to key on: %+v", record.TrackedIgnored)
	}
}

// TestTheInputsRecordNamesAWorktreesSharedExclude: a root whose `.git` is a worktree's file names the info/exclude in
// the common directory its git directory shares, there or absent, since the census reads it (#vdbcswp).
func TestTheInputsRecordNamesAWorktreesSharedExclude(test *testing.T) {
	test.Parallel()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	main, root := filepath.Join(base, "main"), filepath.Join(base, "tree")
	gitDirectory := filepath.Join(main, ".git", "worktrees", "tree")
	for _, directory := range []string{gitDirectory, root} {
		if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
			test.Fatal(makeError)
		}
	}
	for file, contents := range map[string]string{
		filepath.Join(root, ".git"):              "gitdir: " + gitDirectory + "\n",
		filepath.Join(gitDirectory, "commondir"): "../..\n",
	} {
		if writeError := os.WriteFile(file, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	engine := &pipeline{
		request:   invocation{root: root},
		described: &describedProject{workspace: project.Workspace{Root: root}},
	}
	record := engine.inputsRecord(&load.Loaded{})
	shared := filepath.Join(main, ".git", "info", "exclude")
	if !slices.Contains(record.Absent, shared) {
		test.Errorf("the absent paths do not name the shared exclude %s: %q", shared, record.Absent)
	}
	if worktreeOwn := filepath.Join(root, ".git", "info", "exclude"); slices.Contains(record.Absent, worktreeOwn) ||
		slices.Contains(record.Files, worktreeOwn) {
		test.Errorf("the record names %s, below a `.git` that is a file", worktreeOwn)
	}
}
