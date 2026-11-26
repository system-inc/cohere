// Package repository finds files inside this repository from a test, whatever package the test runs in.
//
// A test reads only this repository (Kirk, Nov 25, #5wj23dd): no test reads a snapshot of another one, so
// there is no variable to set and nothing to skip. A path a fixture records is relative to the repository's
// root, so it reads the same on every machine.
package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// module is the module path in this repository's go.mod, which is how Root knows the go.mod it found is this
// repository's rather than a nested module's.
const module = "github.com/system-inc/cohere"

// Root is this repository's root: the nearest directory above the working directory whose go.mod declares
// this module. A test runs in its package's directory, so it finds the root from any package.
func Root() (string, error) {
	directory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		return "", workingDirectoryError
	}
	for {
		contents, readError := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readError == nil {
			for _, line := range strings.Split(string(contents), "\n") {
				if declared, isModule := strings.CutPrefix(
					strings.TrimSpace(line),
					"module ",
				); isModule && strings.TrimSpace(declared) == module {
					return directory, nil
				}
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("no go.mod declaring %s above the working directory", module)
		}
		directory = parent
	}
}

// Path is the file a slash-separated path relative to this repository's root names. It fails the test when
// the path is absolute or climbs out of the repository, since neither reads the same on another machine, and
// when nothing is there.
func Path(test testing.TB, relative string) string {
	test.Helper()
	if filepath.IsAbs(relative) {
		test.Fatalf("%q is an absolute path, which reads the same on no other machine: give it relative to "+
			"this repository", relative)
	}
	root, rootError := Root()
	if rootError != nil {
		test.Fatal(rootError)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	// A path that climbs out of the repository would let a fixture depend on a sibling checkout while looking
	// portable, and read nothing on a machine without one.
	if inside, relativePathError := filepath.Rel(
		root,
		path,
	); relativePathError != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		test.Fatalf("%q climbs out of this repository, so it reads the same on no other machine: give a path "+
			"inside this repository", relative)
	}
	if _, statError := os.Stat(path); statError != nil {
		test.Fatalf("%q is not in this repository: %v", relative, statError)
	}
	return path
}
