package program_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// withinTree is a project whose run read a file above its root, as a Go run reads a staticcheck.conf or a
// .gitignore in a directory above its module; a file outside the root and above none of it, as a worktree's git
// index in the main repository's git directory; and listed a directory holding no file it read.
type withinTree struct {
	above                    string
	root                     string
	source                   string
	listed                   string
	staticcheckConfiguration string
	gitDirectory             string
	gitIndex                 string
}

func newWithinTree(test *testing.T) withinTree {
	test.Helper()
	above := test.TempDir()
	tree := withinTree{
		above:                    above,
		root:                     filepath.Join(above, "module"),
		staticcheckConfiguration: filepath.Join(above, "staticcheck.conf"),
		gitDirectory:             filepath.Join(test.TempDir(), "main", ".git", "worktrees", "module"),
	}
	tree.source = filepath.Join(tree.root, "a.go")
	tree.listed = filepath.Join(tree.root, "notes")
	tree.gitIndex = filepath.Join(tree.gitDirectory, "index")
	for _, directory := range []string{tree.root, tree.listed, tree.gitDirectory} {
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}
	for _, file := range []string{tree.source, tree.staticcheckConfiguration, tree.gitIndex} {
		if writeError := os.WriteFile(file, []byte("x\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return tree
}

// recordedPaths is every path a record holds, so a test can say which directories it keeps.
func recordedPaths(cache *program.RunCache) []string {
	paths := []string{}
	for _, input := range cache.Inputs {
		paths = append(paths, input.Path)
	}
	return paths
}

// TestRecordRunCacheWithinKeepsNoDirectoryOutsideTheRoots: a file read outside the roots, above the module or in a
// git directory elsewhere, is recorded, and the directory holding it is not, while RecordRunCache keeps both as it
// always has. Such a directory moves with everything else in it, a sibling project or the lock file every git
// command writes, so keeping it would make every later run a miss, or a refused record, that nothing explains.
func TestRecordRunCacheWithinKeepsNoDirectoryOutsideTheRoots(test *testing.T) {
	test.Parallel()
	tree := newWithinTree(test)
	files := []string{tree.source, tree.staticcheckConfiguration, tree.gitIndex}
	within, withinError := program.RecordRunCacheWithin([]string{tree.root}, "key", files, []string{tree.listed},
		nil, nil, nil, 0, time.Time{}, nil)
	if withinError != nil {
		test.Fatal(withinError)
	}
	paths := recordedPaths(within)
	for _, outside := range []string{tree.above, tree.gitDirectory} {
		if slices.Contains(paths, outside) {
			test.Errorf("%s, outside the root, was recorded: %q", outside, paths)
		}
	}
	for _, kept := range []string{
		tree.source, tree.staticcheckConfiguration, tree.gitIndex, tree.root, tree.listed,
	} {
		if !slices.Contains(paths, kept) {
			test.Errorf("%s was not recorded: %q", kept, paths)
		}
	}

	everything, recordError := program.RecordRunCache("key", files, []string{tree.listed}, nil, nil, nil, 0,
		time.Time{})
	if recordError != nil {
		test.Fatal(recordError)
	}
	for _, holding := range []string{tree.above, tree.gitDirectory} {
		if !slices.Contains(recordedPaths(everything), holding) {
			test.Errorf("RecordRunCache no longer records %s, which holds a file: %q", holding,
				recordedPaths(everything))
		}
	}
}

// TestRecordRunCacheWithinHoldsEveryListingToTheClock: a listed directory that changed after the run began reading
// refuses the record, though it holds no file the run read, since its listing decided which files exist. A package
// created under it mid-run would otherwise be recorded with the new listing and replayed over by a verdict that
// never saw it.
func TestRecordRunCacheWithinHoldsEveryListingToTheClock(test *testing.T) {
	test.Parallel()
	tree := newWithinTree(test)
	// After the tree was made, at the file system's resolution, and before the package appears.
	time.Sleep(10 * time.Millisecond)
	readSince := time.Now()
	time.Sleep(10 * time.Millisecond)
	if createDirectoryError := os.Mkdir(filepath.Join(tree.listed, "late"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	_, withinError := program.RecordRunCacheWithin([]string{tree.root}, "key", []string{tree.source},
		[]string{tree.listed}, nil, nil, nil, 0, readSince, nil)
	if withinError == nil || !strings.Contains(withinError.Error(), tree.listed) {
		test.Fatalf("a listing changed after the clock was recorded: %v", withinError)
	}
}
