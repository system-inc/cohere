//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling

// Package staticcheck_fixtures is staticcheck's own check fixtures, vendored from honnef.co/go/tools at the release
// go.mod requires (testdata/README.md), so a port's test reads upstream's fixtures from this repository and never
// from the module cache. It is a package of its own, apart from parity, which nearly every port's test imports, so
// only the tests that read the fixtures carry them.
package staticcheck_fixtures

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// vendored is the fixtures as testdata keeps them, embedded so a test finds them wherever it runs and however its
// binary was built: a test runs in its own package's directory, and a -trimpath build keeps no path to this one.
//
//go:embed testdata
var vendored embed.FS

// fixtureSuffix ends the name each fixture's Go file is kept under, since internal/testing/guard holds every .go file
// in the repository to gofmt, testdata included, and fourteen of these are not gofmt-clean, some on purpose: SA9009's
// cases are comments gofmt would move. Every Go file is kept the same way, so one rule names them all.
const fixtureSuffix = ".go.fixture"

// Directory is a directory of the test's own holding the fixtures under the path the elements name in
// honnef.co/go/tools, such as "staticcheck", "sa1019", "testdata", byte for byte as the release keeps them, with each
// Go file under its own name again. The directory returned is laid out as the module lays it, so its base is the last
// element, and with no elements it is every check's fixtures. The copy is the test's, so a test may write beside a
// fixture, as the go.mod staticcheck's testutil lays over a version folder.
func Directory(test *testing.T, elements ...string) string {
	test.Helper()
	root := path.Join(append([]string{"testdata"}, elements...)...)
	if _, statError := fs.Stat(vendored, root); statError != nil {
		test.Fatalf("staticcheck's fixtures hold no %s: %v", path.Join(elements...), statError)
	}
	directory := filepath.Join(append([]string{test.TempDir()}, elements...)...)
	walkError := fs.WalkDir(vendored, root, func(name string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(name, root), "/")
		destination := filepath.Join(directory, filepath.FromSlash(relative))
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		contents, readError := fs.ReadFile(vendored, name)
		if readError != nil {
			return readError
		}
		if stem, isFixture := strings.CutSuffix(destination, fixtureSuffix); isFixture {
			destination = stem + ".go"
		}
		return os.WriteFile(destination, contents, 0o644)
	})
	if walkError != nil {
		test.Fatalf("copying staticcheck's fixtures under %s: %v", path.Join(elements...), walkError)
	}
	return directory
}
