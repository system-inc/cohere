package project

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/git_tree"
	"github.com/system-inc/cohere/internal/testing/written_repository"
)

// gitlinkRepository is a repository in a test's temporary directory holding go.work and go.mod, so
// ReadWorkspace reads it, both tracked, with tracked beside them in its index, written without git.
func gitlinkRepository(test *testing.T, tracked ...git_tree.IndexEntry) string {
	test.Helper()
	repository := test.TempDir()
	files := map[string]string{
		"go.work": "go 1.25\n\nuse .\n",
		"go.mod":  "module example.com/root\n\ngo 1.25\n",
	}
	if writeError := writeFiles(repository, files); writeError != nil {
		test.Fatal(writeError)
	}
	index := append([]git_tree.IndexEntry{trackedFile("go.work"), trackedFile("go.mod")}, tracked...)
	if writeError := written_repository.Write(repository, index...); writeError != nil {
		test.Fatal(writeError)
	}
	return repository
}

// A submodule's path is read as git records it: one holding a tab, which git's plain listing quotes,
// and one holding two spaces in a row, which splitting that listing on whitespace closes up. Each is the
// path exactly, with its pin.
func TestGitlinksOfReadsAPathWithATabOrTwoSpaces(test *testing.T) {
	test.Parallel()
	repository := gitlinkRepository(test,
		gitlink("tab\tmodule", strings.Repeat("1", 40)), gitlink("two  spaces", strings.Repeat("2", 40)))
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
// ReadWorkspace, which reads the gitlinks to leave a submodule's modules out, reads the workspace. The index is
// the one git 2.54's merge left, entry for entry as `git ls-files --stage` listed it: a file committed as "base"
// and then as "theirs" on one branch and "ours" on the other, module pinned at 1s and then at 3s and 2s, and
// removed pinned at 1s, then at 3s by theirs and removed by ours.
func TestGitlinksOfReadsAMergeLeftInConflict(test *testing.T) {
	test.Parallel()
	atStage := func(entry git_tree.IndexEntry, stage int) git_tree.IndexEntry {
		entry.Stage = stage
		return entry
	}
	conflictedFile := func(object string, stage int) git_tree.IndexEntry {
		return git_tree.IndexEntry{Entry: git_tree.Entry{Mode: git_tree.ModeRegular, Object: object, Path: "file"},
			Stage: stage}
	}
	repository := gitlinkRepository(test,
		conflictedFile("df967b96a579e45a18b8251732d16804b2e56a55", 1),
		conflictedFile("b19a1e93bec1317dc6097229e12afaffbfa74dc2", 2),
		conflictedFile("950b81b7eee953d050aa05a641f8e056c85dd1bd", 3),
		atStage(gitlink("module", strings.Repeat("1", 40)), 1),
		atStage(gitlink("module", strings.Repeat("2", 40)), 2),
		atStage(gitlink("module", strings.Repeat("3", 40)), 3),
		atStage(gitlink("removed", strings.Repeat("1", 40)), 1),
		atStage(gitlink("removed", strings.Repeat("3", 40)), 3),
	)

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
