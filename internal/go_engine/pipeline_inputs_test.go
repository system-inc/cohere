package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// The inputs record adds what describing the project read to the load's: the ignore files of the nearest repository
// above the root, there or absent from the repository down to the root, and there in a directory the load names; no
// `.git` between the root and the repository; the repository's info/exclude; the settings file; and a file of ours
// nobody could read. Nothing is said of the directories above the repository, whose ignore files git never reads.
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
		filepath.Join(root, "go.mod"),
		filepath.Join(root, "sub", ".gitignore"),
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
	loaded := &load.Loaded{Inputs: load.Inputs{
		Files:       []string{filepath.Join(root, "go.mod")},
		Directories: []string{root, filepath.Join(root, "sub")},
	}}

	record := engine.inputsRecord(loaded)

	for _, want := range []string{
		filepath.Join(repository, ".gitignore"),
		filepath.Join(root, "sub", ".gitignore"),
		filepath.Join(root, "go.mod"),
		unreadable,
	} {
		if !slices.Contains(record.Files, want) {
			test.Errorf("the files do not name %s: %q", want, record.Files)
		}
	}
	for _, want := range []string{
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, ".git"),
		filepath.Join(root, "go.work"),
		filepath.Join(root, "CohereSettings.json"),
		filepath.Join(repository, ".git", "info", "exclude"),
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
