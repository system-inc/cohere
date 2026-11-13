package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A tracked file a run rewrote, as go list tidies go.work.sum, one it made executable, and one it deleted are each
// put back from the commit, in the outer layer and in a nested one alike, and named within the corpus. An untracked
// file, a cache's, is no file of the commit's: it is neither named nor touched. After it, every status is empty.
func TestRestoreChangedFilesPutsBackWhatARunChanged(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	outerCommit := makeRepository(test, root, map[string]string{
		"go.work.sum": "golang.org/x/mod v0.1.0 h1:first\n",
		"go.mod":      "module outer\n",
		"keep.txt":    "kept\n",
	}, nil)
	nested := filepath.Join(root, "TypeScript")
	nestedCommit := makeRepository(test, nested, map[string]string{"tsc/go.sum": "example v1 h1:pinned\n"}, nil)
	layers := []corpusLayer{
		{repository: root, commit: outerCommit},
		{repository: nested, commit: nestedCommit, within: "TypeScript"},
	}

	tidied := "golang.org/x/mod v0.1.0 h1:first\ngolang.org/x/tools v0.2.0 h1:added\n"
	for name, contents := range map[string]string{
		filepath.Join(root, "go.work.sum"):         tidied,
		filepath.Join(nested, "tsc", "go.sum"):     "example v1 h1:tidied\n",
		filepath.Join(root, ".cache", "entry.bin"): "a cache the run wrote\n",
	} {
		if createParentError := os.MkdirAll(filepath.Dir(name), 0o755); createParentError != nil {
			test.Fatal(createParentError)
		}
		if writeError := os.WriteFile(name, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if changeModeError := os.Chmod(filepath.Join(root, "keep.txt"), 0o755); changeModeError != nil {
		test.Fatal(changeModeError)
	}
	if removeError := os.Remove(filepath.Join(root, "go.mod")); removeError != nil {
		test.Fatal(removeError)
	}

	restored, restoreError := restoreChangedFiles(root, layers)
	if restoreError != nil {
		test.Fatal(restoreError)
	}
	if want := []string{"go.mod", "go.work.sum", "keep.txt", "TypeScript/tsc/go.sum"}; !slices.Equal(restored, want) {
		test.Errorf("put back %q, want %q", restored, want)
	}
	for name, want := range map[string]string{
		filepath.Join(root, "go.work.sum"):         "golang.org/x/mod v0.1.0 h1:first\n",
		filepath.Join(root, "go.mod"):              "module outer\n",
		filepath.Join(nested, "tsc", "go.sum"):     "example v1 h1:pinned\n",
		filepath.Join(root, ".cache", "entry.bin"): "a cache the run wrote\n",
	} {
		if contents, readError := os.ReadFile(name); readError != nil || string(contents) != want {
			test.Errorf("%s reads %q, %v; want %q", name, contents, readError, want)
		}
	}
	if information, statError := os.Stat(filepath.Join(root, "keep.txt")); statError != nil ||
		information.Mode().Perm() != 0o644 {
		test.Errorf("keep.txt's mode was not put back: %v, %v", information, statError)
	}
	for _, directory := range []string{root, nested} {
		if status := runGit(test, directory, "status", "--porcelain", "--untracked-files=no",
			"--ignore-submodules=all"); status != "" {
			test.Errorf("%s still has changes:\n%s", directory, status)
		}
	}
	if again, againError := restoreChangedFiles(root, layers); againError != nil || len(again) != 0 {
		test.Errorf("a corpus put back puts back %q again, %v", again, againError)
	}
}
