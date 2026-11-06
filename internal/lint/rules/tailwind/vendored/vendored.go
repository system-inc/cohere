// Package vendored is the installed tailwindcss the Tailwind rules' tests build a design system against: a
// snapshot of tailwindcss 4.3.3 in testdata, so those tests run on any machine instead of only where ahra's
// node_modules happens to be (#sycrdr6).
//
// Only the files the design-system loader reads are kept: index.css and the stylesheets it imports
// (theme.css, preflight.css, utilities.css), with package.json and the package's MIT LICENSE beside them.
// See testdata/README.md for the source and how to refresh it.
package vendored

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TailwindVersion is the version of tailwindcss the snapshot holds.
const TailwindVersion = "4.3.3"

// modulePath is cohere's module, which an import-path source file name begins with.
const modulePath = "github.com/system-inc/cohere"

// TailwindPackageRoot is the snapshot's directory, the root a node_modules/tailwindcss would be. It fails when
// the working directory cannot be read, or when a -trimpath build's working directory is outside cohere's module.
func TailwindPackageRoot() (string, error) {
	_, file, _, _ := runtime.Caller(0)
	working, workingError := os.Getwd()
	if workingError != nil {
		return "", fmt.Errorf("reading the working directory: %w", workingError)
	}
	directory, sourceDirectoryError := sourceDirectory(file, working)
	if sourceDirectoryError != nil {
		return "", sourceDirectoryError
	}
	return filepath.Join(directory, "testdata", "tailwindcss"), nil
}

// TailwindPackageRootForTest is TailwindPackageRoot for a test, which fails the test when the snapshot cannot be
// found, every caller of the snapshot being a test that has nothing to run without it.
func TailwindPackageRootForTest(test *testing.T) string {
	test.Helper()
	root, rootError := TailwindPackageRoot()
	if rootError != nil {
		test.Fatalf("locating the tailwindcss snapshot: %v", rootError)
	}
	return root
}

// sourceDirectory is the directory of the source file runtime.Caller named. Built without -trimpath that is a
// path on disk. Built with it, which cohere-dev does for every go command, it is the file's import path, so
// it is found under cohere's module root: the nearest directory above working whose go.mod is cohere's. The
// tests that call this run in their own package's directory, which is inside the module.
func sourceDirectory(file string, working string) (string, error) {
	if filepath.IsAbs(file) {
		return filepath.Dir(file), nil
	}
	relative, inModule := strings.CutPrefix(filepath.ToSlash(file), modulePath+"/")
	if !inModule {
		return "", fmt.Errorf("%s is neither a path on disk nor in %s", file, modulePath)
	}
	for directory := working; ; directory = filepath.Dir(directory) {
		if contents, readError := os.ReadFile(
			filepath.Join(directory, "go.mod"),
		); readError == nil && strings.HasPrefix(string(contents), "module "+modulePath+"\n") {
			return filepath.Join(directory, filepath.FromSlash(filepath.Dir(relative))), nil
		}
		if filepath.Dir(directory) == directory {
			return "", fmt.Errorf("no go.mod for %s above %s, so %s can't be found", modulePath, working, file)
		}
	}
}
