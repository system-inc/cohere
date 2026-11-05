package testpolicy

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// eachTestFile calls visit with every _test.go file of this module and its source, and returns the module
// root and how many files it read. It fails the test when it read fewer than 100, since a walk that read
// nothing would let every policy pass.
func eachTestFile(test *testing.T, visit func(path string, source []byte) error) (string, int) {
	test.Helper()
	root, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	files := 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "TypeScript", "testdata", "node_modules", ".cache", ".git", ".build":
				return filepath.SkipDir
			}
			// A directory with a go.mod of its own is another module (the TypeScript shims), which this
			// module's go test ./... never runs, so its tests are not this module's policies to hold.
			if path != root {
				if _, statError := os.Stat(filepath.Join(path, "go.mod")); statError == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		files++
		return visit(path, source)
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if files < 100 {
		test.Fatalf("read only %d test files under %s", files, root)
	}
	return root, files
}
