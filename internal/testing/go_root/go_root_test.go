package go_root

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The root found is the toolchain's: it holds the runtime's source, which a standard-library import reads through
// go/build.
func TestTheRootFoundHoldsTheRuntimeSource(test *testing.T) {
	test.Parallel()
	root := GoRoot(test)
	information, statError := os.Stat(filepath.Join(root, "src", "runtime", "extern.go"))
	if statError != nil || !information.Mode().IsRegular() {
		test.Fatalf("the root found, %s, holds no src/runtime/extern.go: %v", root, statError)
	}
}

// A directory is a root only with a go in its bin and the runtime's source: one lacking either is refused, naming
// what is missing, never taken.
func TestARootWithoutItsGoOrItsRuntimeIsRefused(test *testing.T) {
	test.Parallel()
	goName := "go"
	if runtime.GOOS == "windows" {
		goName = "go.exe"
	}
	root := test.TempDir()
	if checkError := checkRoot(root, "the fixture"); checkError == nil ||
		!strings.Contains(checkError.Error(), filepath.Join(root, "bin", goName)) {
		test.Errorf("an empty directory was not refused for its missing go: %v", checkError)
	}
	if makeError := os.MkdirAll(filepath.Join(root, "bin"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "bin", goName), nil, 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	if checkError := checkRoot(root, "the fixture"); checkError == nil ||
		!strings.Contains(checkError.Error(), filepath.Join(root, "src", "runtime")) {
		test.Errorf("a directory with a go but no runtime source was not refused for it: %v", checkError)
	}
	if makeError := os.MkdirAll(filepath.Join(root, "src", "runtime"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if checkError := checkRoot(root, "the fixture"); checkError != nil {
		test.Errorf("a directory with a go and the runtime's source was refused: %v", checkError)
	}
}
