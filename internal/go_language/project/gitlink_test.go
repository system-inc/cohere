package project

import (
	"slices"
	"strings"
	"testing"
)

// gitlinkRepository is a repository in a test's temporary directory holding go.work and go.mod, so
// ReadWorkspace reads it, committed once.
func gitlinkRepository(test *testing.T) string {
	test.Helper()
	repository := test.TempDir()
	files := map[string]string{
		"go.work": "go 1.25\n\nuse .\n",
		"go.mod":  "module example.com/root\n\ngo 1.25\n",
		"file":    "base\n",
	}
	if writeError := writeFiles(repository, files); writeError != nil {
		test.Fatal(writeError)
	}
	for _, arguments := range [][]string{{"init", "-q", "-b", "main"}, {"add", "."}, {"commit", "-q", "-m", "base"}} {
		if gitError := runGit(repository, arguments...); gitError != nil {
			test.Fatal(gitError)
		}
	}
	return repository
}

// pinGitlink records path in repository's index as a gitlink pinned at a commit of digit repeated, one
// no repository holds: GitlinksOf reads the index alone, as git merges a gitlink by its id alone.
func pinGitlink(test *testing.T, repository string, path string, digit string) {
	test.Helper()
	cacheInfo := "160000," + strings.Repeat(digit, 40) + "," + path
	if gitError := runGit(repository, "update-index", "--add", "--cacheinfo", cacheInfo); gitError != nil {
		test.Fatal(gitError)
	}
}

// A submodule's path is read as git records it: one holding a tab, which git's plain listing quotes,
// and one holding two spaces in a row, which splitting that listing on whitespace closes up. Each is the
// path exactly, with its pin.
func TestGitlinksOfReadsAPathWithATabOrTwoSpaces(test *testing.T) {
	test.Parallel()
	repository := gitlinkRepository(test)
	pinGitlink(test, repository, "tab\tmodule", "1")
	pinGitlink(test, repository, "two  spaces", "2")
	links, linksError := GitlinksOf(repository)
	if linksError != nil {
		test.Fatal(linksError)
	}
	want := []Gitlink{
		{Path: "tab\tmodule", Pin: strings.Repeat("1", 40)},
		{Path: "two  spaces", Pin: strings.Repeat("2", 40)},
	}
	if !slices.Equal(links, want) {
		test.Errorf("GitlinksOf read %q, want %q", links, want)
	}
}

// A worktree mid-merge still reads. The merge left a file in conflict, a submodule both sides moved (a
// gitlink at stages 1, 2 and 3) and a submodule ours removed and theirs moved (1 and 3 only). Each
// submodule is listed once: the first pinned at ours, the second, which ours no longer holds, at theirs.
// ReadWorkspace, which reads the gitlinks to leave a submodule's modules out, reads the workspace.
func TestGitlinksOfReadsAMergeLeftInConflict(test *testing.T) {
	test.Parallel()
	repository := gitlinkRepository(test)
	// Committed without -a, which would stage each gitlink, whose directory isn't on disk, as removed.
	commit := func(message string) {
		test.Helper()
		for _, arguments := range [][]string{{"add", "file"}, {"commit", "-q", "-m", message}} {
			if gitError := runGit(repository, arguments...); gitError != nil {
				test.Fatal(gitError)
			}
		}
	}
	write := func(content string) {
		test.Helper()
		if writeError := writeFiles(repository, map[string]string{"file": content}); writeError != nil {
			test.Fatal(writeError)
		}
	}
	checkout := func(arguments ...string) {
		test.Helper()
		if gitError := runGit(repository, append([]string{"checkout", "-q"}, arguments...)...); gitError != nil {
			test.Fatal(gitError)
		}
	}
	pinGitlink(test, repository, "module", "1")
	pinGitlink(test, repository, "removed", "1")
	commit("submodules")
	checkout("-b", "theirs")
	write("theirs\n")
	pinGitlink(test, repository, "module", "3")
	pinGitlink(test, repository, "removed", "3")
	commit("theirs")
	checkout("main")
	write("ours\n")
	pinGitlink(test, repository, "module", "2")
	if gitError := runGit(repository, "update-index", "--force-remove", "removed"); gitError != nil {
		test.Fatal(gitError)
	}
	commit("ours")
	if gitError := runGit(repository, "merge", "-q", "theirs"); gitError == nil {
		test.Fatal("the merge succeeded, and the fixture means it to stop in conflict")
	}

	links, linksError := GitlinksOf(repository)
	if linksError != nil {
		test.Fatalf("a worktree mid-merge was not read: %v", linksError)
	}
	want := []Gitlink{{Path: "module", Pin: strings.Repeat("2", 40)}, {Path: "removed", Pin: strings.Repeat("3", 40)}}
	if !slices.Equal(links, want) {
		test.Errorf("GitlinksOf read %q, want %q", links, want)
	}
	covered, readError := ReadWorkspace(repository)
	if readError != nil {
		test.Fatalf("a workspace mid-merge was not read: %v", readError)
	}
	if !slices.Equal(covered.Submodules, []string{"module", "removed"}) {
		test.Errorf("ReadWorkspace read submodules %q, want module and removed", covered.Submodules)
	}
}
