// Package lookalike holds shapes that look like an undeclared walk outside the package and are not: a path that
// leaves the package to open one fixed file, a walk of another package's that shares the standard library's name, and
// a loop up from the working directory that looks for something other than go.mod.
package lookalike

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	fakefilepath "lookalike/filepath"
)

func visit(path string, entry fs.DirEntry, walkError error) error { return walkError }

// The root leaves the package only to name one fixed file, which no walk lists.
const repository = "../../.."

func fixedFile() {
	runner, absoluteError := filepath.Abs(filepath.Join(repository, "oracle", "node.mjs"))
	if absoluteError != nil {
		return
	}
	_ = exec.Command("node", runner).Run()
	_, _ = os.ReadFile("../../cloud/fast-gate/product-order.json")
	_ = readOracle(filepath.Join("..", ".."))
}

func readOracle(root string) error {
	_, readError := os.ReadFile(filepath.Join(root, "oracle", "adamic.mjs"))
	return readError
}

// A WalkDir of the same name at another package path is not the one the rule knows.
func otherPackagesWalk() {
	_ = fakefilepath.WalkDir("../..", visit)
	_, _ = fakefilepath.Glob("../*")
}

// A method and a function of this package spelled like the standard library's walks walk nothing the rule knows.
type tree struct{}

func (tree) WalkDir(root string, visit fs.WalkDirFunc) error { return nil }

func ReadDir(name string) ([]fs.DirEntry, error) { return nil, nil }

func walksOfTheSameName() {
	_ = tree{}.WalkDir("../..", visit)
	_, _ = ReadDir("../../internal")
}

// A path out of the package that is only printed is never walked.
func printedPath() {
	fmt.Println("../..")
}

// A loop up through filepath.Dir that looks for something other than go.mod is not the walk to the repository root.
func nearestTestdata() string {
	directory, _ := os.Getwd()
	for {
		if _, statError := os.Stat(filepath.Join(directory, "testdata")); statError == nil {
			return directory
		}
		directory = filepath.Dir(directory)
	}
}

func walksNearestTestdata() {
	_ = filepath.WalkDir(nearestTestdata(), visit)
}

// The repository root found by walking up to go.mod, used to read one fixed file, is no walk.
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

func readsOneFileAtTheRoot(test *testing.T) {
	_, _ = os.ReadFile(filepath.Join(repositoryRoot(test), "go.sum"))
}
