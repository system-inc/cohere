package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A binary already installed under its hash is never replaced, not even by different bytes. A run reads
// the binary as one of its inputs, so a second install's replacement under a run already reading it gave
// the binary a new identity and times, and the run refused its record (#p5ndwqa).
func TestInstallingTheSwiftBinaryKeepsABinaryAlreadyThere(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	source := filepath.Join(directory, "product")
	destination := filepath.Join(directory, "cohere-swift-hash")
	writeFile(test, source, "the second build")
	writeFile(test, destination, "the first build")
	before, statError := os.Stat(destination)
	if statError != nil {
		test.Fatal(statError)
	}

	if copyExecutableError := copyExecutable(source, destination); copyExecutableError != nil {
		test.Fatalf("installing over a binary already there: %v", copyExecutableError)
	}

	after, statError := os.Stat(destination)
	if statError != nil {
		test.Fatal(statError)
	}
	if !os.SameFile(before, after) {
		test.Error("the installed binary was replaced by another file, so a run reading it would see it change")
	}
	if !after.ModTime().Equal(before.ModTime()) {
		test.Errorf("the installed binary's modification time moved from %v to %v", before.ModTime(), after.ModTime())
	}
	if contents := readFile(test, destination); contents != "the first build" {
		test.Errorf("the installed binary now holds %q", contents)
	}
	expectNoPartialFiles(test, directory)
}

// A missing binary is installed whole and executable, with the product's bytes.
func TestInstallingTheSwiftBinaryPutsTheProductInPlace(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	source := filepath.Join(directory, "product")
	destination := filepath.Join(directory, "cohere-swift-hash")
	writeFile(test, source, "the build")

	if copyExecutableError := copyExecutable(source, destination); copyExecutableError != nil {
		test.Fatalf("installing a missing binary: %v", copyExecutableError)
	}

	info, statError := os.Stat(destination)
	if statError != nil {
		test.Fatal(statError)
	}
	if info.Mode().Perm()&0o111 == 0 {
		test.Errorf("the installed binary is not executable: %v", info.Mode())
	}
	if contents := readFile(test, destination); contents != "the build" {
		test.Errorf("the installed binary holds %q", contents)
	}
	expectNoPartialFiles(test, directory)
}

// Processes that find cohere-swift missing at once and all install it leave one binary, whole, and nothing
// else.
func TestInstallingTheSwiftBinaryConcurrentlyLeavesOneBinary(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	source := filepath.Join(directory, "product")
	destination := filepath.Join(directory, "cohere-swift-hash")
	writeFile(test, source, "the build")

	installers := 8
	errorsByInstaller := make([]error, installers)
	var group sync.WaitGroup
	for installer := range installers {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsByInstaller[installer] = copyExecutable(source, destination)
		}()
	}
	group.Wait()

	for installer, installError := range errorsByInstaller {
		if installError != nil {
			test.Errorf("installer %d: %v", installer, installError)
		}
	}
	if contents := readFile(test, destination); contents != "the build" {
		test.Errorf("the installed binary holds %q", contents)
	}
	expectNoPartialFiles(test, directory)
}

func readFile(test *testing.T, path string) string {
	test.Helper()

	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatalf("reading %s: %v", path, readError)
	}
	return string(contents)
}

// expectNoPartialFiles fails the test if an install left a temporary file in directory.
func expectNoPartialFiles(test *testing.T, directory string) {
	test.Helper()

	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".partial-") {
			test.Errorf("an install left %s behind", entry.Name())
		}
	}
}
