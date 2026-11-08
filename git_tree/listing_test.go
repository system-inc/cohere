package git_tree_test

import (
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
