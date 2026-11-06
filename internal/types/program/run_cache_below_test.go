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

// belowTree is a project whose run read a file above its root, as a Go run reads a staticcheck.conf or a
// .gitignore in a directory above its module, and listed a directory holding no file it read.
type belowTree struct {
	above         string
	root          string
	source        string
	listed        string
	configuration string
}

func newBelowTree(test *testing.T) belowTree {
	test.Helper()
	above := test.TempDir()
	tree := belowTree{
		above:         above,
		root:          filepath.Join(above, "module"),
		configuration: filepath.Join(above, "staticcheck.conf"),
	}
	tree.source = filepath.Join(tree.root, "a.go")
	tree.listed = filepath.Join(tree.root, "notes")
	for _, directory := range []string{tree.root, tree.listed} {
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}
	for _, file := range []string{tree.source, tree.configuration} {
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

// TestRecordRunCacheBelowKeepsNoDirectoryAboveTheRoot: a file read above the root is recorded, and the directory
// holding it is not, while RecordRunCache keeps it as it always has. The directory above a module moves with
// everything beside the project, so keeping it would make every later run a miss that nothing explains.
func TestRecordRunCacheBelowKeepsNoDirectoryAboveTheRoot(test *testing.T) {
	test.Parallel()
	tree := newBelowTree(test)
	files := []string{tree.source, tree.configuration}
	below, belowError := program.RecordRunCacheBelow([]string{tree.root}, "key", files, []string{tree.listed}, nil,
		nil, nil, 0, time.Time{})
	if belowError != nil {
		test.Fatal(belowError)
	}
	paths := recordedPaths(below)
	if slices.Contains(paths, tree.above) {
		test.Errorf("the directory above the root was recorded: %q", paths)
	}
	for _, kept := range []string{tree.source, tree.configuration, tree.root, tree.listed} {
		if !slices.Contains(paths, kept) {
			test.Errorf("%s was not recorded: %q", kept, paths)
		}
	}

	everything, recordError := program.RecordRunCache("key", files, []string{tree.listed}, nil, nil, nil, 0,
		time.Time{})
	if recordError != nil {
		test.Fatal(recordError)
	}
	if !slices.Contains(recordedPaths(everything), tree.above) {
		test.Errorf("RecordRunCache no longer records the directory holding every file: %q", recordedPaths(everything))
	}
}

// TestRecordRunCacheBelowHoldsEveryListingToTheClock: a listed directory that changed after the run began reading
// refuses the record, though it holds no file the run read, since its listing decided which files exist. A package
// created under it mid-run would otherwise be recorded with the new listing and replayed over by a verdict that
// never saw it.
func TestRecordRunCacheBelowHoldsEveryListingToTheClock(test *testing.T) {
	test.Parallel()
	tree := newBelowTree(test)
	// After the tree was made, at the file system's resolution, and before the package appears.
	time.Sleep(10 * time.Millisecond)
	readSince := time.Now()
	time.Sleep(10 * time.Millisecond)
	if createDirectoryError := os.Mkdir(filepath.Join(tree.listed, "late"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	_, belowError := program.RecordRunCacheBelow([]string{tree.root}, "key", []string{tree.source},
		[]string{tree.listed}, nil, nil, nil, 0, readSince)
	if belowError == nil || !strings.Contains(belowError.Error(), tree.listed) {
		test.Fatalf("a listing changed after the clock was recorded: %v", belowError)
	}
}
