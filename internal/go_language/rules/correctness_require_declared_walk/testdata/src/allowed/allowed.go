// Package allowed holds walks the rule must leave alone: each stays in the package, can't be traced, or is declared.
package allowed

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"build_cache"
)

func visit(path string, entry fs.DirEntry, walkError error) error { return walkError }

func temporaryDirectory(test *testing.T) {
	_ = filepath.WalkDir(test.TempDir(), visit)
	_ = walkUnder(test.TempDir())
	_ = walkUnder(os.TempDir())
}

func walkUnder(root string) error {
	return filepath.WalkDir(root, visit)
}

func thisDirectory() {
	_ = filepath.WalkDir(".", visit)
	_, _ = filepath.Glob("*.go")
	_, _ = os.ReadDir("subdirectory")
	_ = walkUnder(filepath.Join(".", "subdirectory"))
	// A path that climbs out and back in names a directory of this package.
	_, _ = os.ReadDir("subdirectory/../other")
}

func ownTestdata() {
	_ = filepath.WalkDir("testdata", visit)
	_, _ = filepath.Glob(filepath.Join("testdata", "*.golden"))
	_ = walkUnder("testdata/corpus")
}

// The working directory is the package directory itself, so walking it stays in the package.
func workingDirectory() {
	working, workingError := os.Getwd()
	if workingError != nil {
		return
	}
	_ = filepath.WalkDir(working, visit)
}

func declaredOnTheLine() {
	_ = filepath.WalkDir("../..", visit) // Reads: internal/**/*.go
}

func declaredAbove() {
	// Reads: internal/**/*.go
	_ = filepath.WalkDir("../..", visit)
}

func declaredBelowAReason() {
	// The census reads every package's Go files, so it names them for the planner.
	// Reads: cmd/**/*.go
	// Reads: internal/**/*.go
	_ = walkUnder(filepath.Join("..", ".."))
}

// A marker with no glob beside one that names a glob leaves the walk declared by the one that does.
func declaredBesideAnEmptyMarker() {
	// Reads:
	// Reads: internal/**/*.go
	_ = filepath.WalkDir("../..", visit)
}

func declaredWalkUp(test *testing.T) {
	// Reads: **/*_test.go
	_ = walkUnder(repositoryRoot(test))
}

func repositoryRoot(test *testing.T) string {
	directory, workingError := os.Getwd()
	if workingError != nil {
		test.Fatal(workingError)
	}
	for {
		if _, statError := os.Stat(filepath.Join(directory, "go.mod")); statError == nil {
			return directory
		}
		directory = filepath.Dir(directory)
	}
}

// A root ranged out of a slice can't be traced, and the rule never guesses.
func untraced(roots []string) {
	for _, root := range roots {
		_ = filepath.WalkDir(root, visit)
	}
}

// A directory another package computes is outside every package, but the rule can't trace it, so it is not flagged.
func productDirectory() {
	_ = filepath.WalkDir(build_cache.Product("native-runtime"), visit)
}

// A path out to the repository and back into this package's own directory stays in it.
const reviewRoot = "../allowed" + "/testdata/review"

func backIntoThisPackage() {
	_, _ = filepath.Glob(filepath.Join("..", "allowed", "testdata", "*.a"))
	_, _ = filepath.Glob(filepath.Join(reviewRoot, "agree", "*.a"))
	directory := "allowed/testdata/refused"
	_, _ = os.ReadDir(filepath.Join("..", directory))
	_ = globUnder("../allowed")
	_, _ = os.ReadDir(filepath.Dir("../allowed/testdata"))
}

func globUnder(root string) []string {
	paths, _ := filepath.Glob(filepath.Join(root, "testdata", "*.a"))
	return paths
}
