package git_tree_test

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/git_tree"
)

// A listing git wouldn't print is refused, record by record, never read as some other tree.
func TestAListingGitWouldntPrintIsRefused(test *testing.T) {
	test.Parallel()
	blob := strings.Repeat("a", 40)
	for name, listing := range map[string]string{
		"a mode git doesn't record":      "100664 blob " + blob + "\tx\x00",
		"a kind its mode disagrees with": "160000 blob " + blob + "\tx\x00",
		"an object that isn't a name":    "100644 blob " + strings.Repeat("A", 40) + "\tx\x00",
		"no tab before the path":         "100644 blob " + blob + " x\x00",
		"two spaces in the header":       "100644  blob " + blob + "\tx\x00",
		"a path leaving the repository":  "100644 blob " + blob + "\t../x\x00",
		"a path with an empty element":   "100644 blob " + blob + "\ta//x\x00",
		"an empty path":                  "100644 blob " + blob + "\t\x00",
		"the last record cut short":      "100644 blob " + blob + "\tx",
		"two object formats": "100644 blob " +
			blob +
			"\tx\x00100644 blob " +
			strings.Repeat("b", 64) +
			"\ty\x00",
	} {
		if entries, parseError := git_tree.ParseTreeListing([]byte(listing)); parseError == nil {
			test.Errorf("%s: read as %+v", name, entries)
		}
	}
	for name, listing := range map[string]string{
		"an unmerged entry":              "100644 " + blob + " 2\tx\x00",
		"a sparse index's directory":     "040000 " + blob + " 0\tx\x00",
		"ls-tree's record, not ls-files": "100644 blob " + blob + "\tx\x00",
	} {
		if entries, parseError := git_tree.ParseStageListing([]byte(listing)); parseError == nil {
			test.Errorf("%s: read as %+v", name, entries)
		}
	}
	for name, listing := range map[string]string{
		"a stage git doesn't record":      "100644 " + blob + " 4\tx\x00",
		"a stage written with two digits": "100644 " + blob + " 01\tx\x00",
		"a sparse index's directory":      "040000 " + blob + " 0\tx\x00",
		"ls-tree's record, not ls-files":  "100644 blob " + blob + "\tx\x00",
		"the last record cut short":       "100644 " + blob + " 2\tx",
	} {
		if entries, parseError := git_tree.ParseIndexListing([]byte(listing)); parseError == nil {
			test.Errorf("%s: read as %+v", name, entries)
		}
	}
	// A tab in a name is part of the path, and the empty listing is no entries.
	entries, parseError := git_tree.ParseStageListing([]byte("100755 " + blob + " 0\ta\tb\x00"))
	if parseError != nil ||
		len(entries) != 1 ||
		entries[0].Path != "a\tb" ||
		entries[0].Mode != git_tree.ModeExecutable {
		test.Errorf("a name with a tab reads as %+v (%v)", entries, parseError)
	}
	if entries, parseError := git_tree.ParseTreeListing(nil); parseError != nil || len(entries) != 0 {
		test.Errorf("the empty listing reads as %+v (%v)", entries, parseError)
	}
}

// TreeId refuses entries no tree holds: a path twice, a path both a file and a directory, an object of
// another format, and a listed directory whose entries rebuild another tree.
func TestTreeIdRefusesEntriesNoTreeHolds(test *testing.T) {
	test.Parallel()
	blob := strings.Repeat("a", 40)
	for name, entries := range map[string][]git_tree.Entry{
		"a path twice": {
			{Mode: git_tree.ModeRegular, Object: blob, Path: "x"},
			{Mode: git_tree.ModeExecutable, Object: blob, Path: "x"},
		},
		"a file and a directory": {
			{Mode: git_tree.ModeRegular, Object: blob, Path: "a"},
			{Mode: git_tree.ModeRegular, Object: blob, Path: "a.b"},
			{Mode: git_tree.ModeRegular, Object: blob, Path: "a/x"},
		},
		"a gitlink with files under it": {
			{Mode: git_tree.ModeGitlink, Object: blob, Path: "sub"},
			{Mode: git_tree.ModeRegular, Object: blob, Path: "sub/x"},
		},
		"another format's object": {
			{Mode: git_tree.ModeRegular, Object: strings.Repeat("a", 64), Path: "x"},
		},
		"a listed directory of another tree": {
			{Mode: git_tree.ModeTree, Object: blob, Path: "a"},
			{Mode: git_tree.ModeRegular, Object: blob, Path: "a/x"},
		},
	} {
		if tree, treeError := git_tree.TreeId(git_tree.Sha1, entries); treeError == nil {
			test.Errorf("%s: rebuilt as %s", name, tree)
		}
	}
}

// listedStages is the repository's index as git prints it without -z, one "<mode> <object> <stage>",
// tab, path line per entry, each path git quoted read back by Go's unquoting: git quotes a path in C's
// spelling (\t, \n, \" and octal), which strconv.Unquote reads. It is the oracle ParseIndexListing's
// entries are compared against, a reading of another listing than the one it parses.
func listedStages(test *testing.T, repository *testRepository) []string {
	test.Helper()
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSuffix(string(repository.git("ls-files", "--stage")), "\n"), "\n") {
		header, path, found := strings.Cut(line, "\t")
		if !found {
			test.Fatalf("git ls-files --stage printed %q, with no tab", line)
		}
		if strings.HasPrefix(path, `"`) {
			unquoted, unquoteError := strconv.Unquote(path)
			if unquoteError != nil {
				test.Fatalf("git ls-files --stage quoted %s, which doesn't unquote: %v", path, unquoteError)
			}
			path = unquoted
		}
		lines = append(lines, header+"\t"+path)
	}
	return lines
}

// indexLines is entries spelled as listedStages spells git's.
func indexLines(entries []git_tree.IndexEntry) []string {
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, fmt.Sprintf("%s %s %d\t%s", entry.Mode, entry.Object, entry.Stage, entry.Path))
	}
	return lines
}

// index is the repository's index, as ParseIndexListing reads git's own listing of it.
func index(test *testing.T, repository *testRepository) []git_tree.IndexEntry {
	test.Helper()
	entries, parseError := git_tree.ParseIndexListing(repository.git("ls-files", "--stage", "-z"))
	if parseError != nil {
		test.Fatal(parseError)
	}
	return entries
}

// A clean index reads as git lists it, every entry at stage 0, and as ParseStageListing reads it: the
// fixture's odd name, links, gitlinks and executables each by the stage and mode git printed.
func TestParseIndexListingReadsACleanIndexAsGitListsIt(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := fixture(test, format)
		entries := index(test, repository)
		if got, want := indexLines(entries), listedStages(test, repository); !slices.Equal(got, want) {
			test.Errorf("ParseIndexListing read\n%q\nand git ls-files --stage lists\n%q", got, want)
		}
		staged := stage(test, repository)
		if len(entries) != len(staged) {
			test.Fatalf("ParseIndexListing read %d entries, and ParseStageListing %d", len(entries), len(staged))
		}
		for position, entry := range entries {
			if entry.Stage != 0 || entry.Entry != staged[position] {
				test.Errorf("entry %d reads as %+v, and ParseStageListing read %+v", position, entry, staged[position])
			}
		}
	})
}

// A merge left in conflict reads, each conflicted path once per stage git holds it at, as git lists it:
// a file both sides changed (stages 1, 2 and 3), a submodule both sides moved (a gitlink at 1, 2 and
// 3), and a submodule ours removed and theirs moved (1 and 3), beside a merged file at stage 0.
// ParseStageListing, which reads only a merged index, refuses the same listing.
func TestParseIndexListingReadsAMergeLeftInConflict(test *testing.T) {
	test.Parallel()
	formats(test, func(test *testing.T, format git_tree.ObjectFormat) {
		repository := newTestRepository(test, format)
		repository.write("merged", "merged\n", 0o644)
		repository.write("conflicted", "base\n", 0o644)
		repository.git("add", "-A")
		repository.git("commit", "-q", "-m", "files")
		// Each pin is a commit no repository holds, as the fixture's gitlink is: git merges a gitlink by
		// its id alone, and one it can't look into is a conflict.
		digits := len(repository.line("rev-parse", "HEAD"))
		pin := func(digit string) string { return strings.Repeat(digit, digits) }
		gitlink := func(digit string, path string) {
			repository.git("update-index", "--add", "--cacheinfo", "160000,"+pin(digit)+","+path)
		}
		gitlink("1", "module")
		gitlink("1", "removed")
		repository.git("commit", "-q", "-m", "base")
		repository.git("checkout", "-q", "-b", "theirs")
		repository.write("conflicted", "theirs\n", 0o644)
		repository.git("add", "conflicted")
		gitlink("3", "module")
		gitlink("3", "removed")
		repository.git("commit", "-q", "-m", "theirs")
		repository.git("checkout", "-q", "main")
		repository.write("conflicted", "ours\n", 0o644)
		repository.git("add", "conflicted")
		gitlink("2", "module")
		repository.git("update-index", "--force-remove", "removed")
		repository.git("commit", "-q", "-m", "ours")
		repository.gitFails("merge", "-q", "theirs")

		entries := index(test, repository)
		if got, want := indexLines(entries), listedStages(test, repository); !slices.Equal(got, want) {
			test.Errorf("ParseIndexListing read\n%q\nand git ls-files --stage lists\n%q", got, want)
		}
		stages := map[string][]int{}
		for _, entry := range entries {
			stages[entry.Path] = append(stages[entry.Path], entry.Stage)
			if (entry.Path == "module" || entry.Path == "removed") &&
				(entry.Mode != git_tree.ModeGitlink || entry.Object != pin(strconv.Itoa(entry.Stage))) {
				test.Errorf("%s at stage %d reads as %s %s, and the merge pinned %s there",
					entry.Path, entry.Stage, entry.Mode, entry.Object, pin(strconv.Itoa(entry.Stage)))
			}
		}
		want := map[string][]int{"conflicted": {1, 2, 3}, "merged": {0}, "module": {1, 2, 3}, "removed": {1, 3}}
		for path, wantStages := range want {
			if !slices.Equal(stages[path], wantStages) {
				test.Errorf("%s reads at stages %v, and the merge left it at %v", path, stages[path], wantStages)
			}
		}
		if len(stages) != len(want) {
			test.Errorf("the index reads as paths %v, and the merge left %v", stages, want)
		}
		if staged, parseError := git_tree.ParseStageListing(
			repository.git("ls-files", "--stage", "-z"),
		); parseError == nil {
			test.Errorf("ParseStageListing read an index in conflict as %+v", staged)
		}
	})
}
