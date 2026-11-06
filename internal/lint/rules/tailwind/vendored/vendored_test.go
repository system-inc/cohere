package vendored

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The snapshot is the version it says, and holds every file the loader reads, so a refresh that drops one
// fails here rather than as a design system that quietly builds from less.
func TestTheSnapshotIsWholeAndTheVersionItSays(test *testing.T) {
	test.Parallel()
	root := TailwindPackageRootForTest(test)
	for _, name := range []string{
		"index.css",
		"theme.css",
		"preflight.css",
		"utilities.css",
		"LICENSE",
		"package.json",
	} {
		if _, statError := os.Stat(filepath.Join(root, name)); statError != nil {
			test.Errorf("the snapshot is missing %s: %v", name, statError)
		}
	}
	contents, readError := os.ReadFile(filepath.Join(root, "package.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		License string `json:"license"`
	}
	if decodeError := json.Unmarshal(contents, &manifest); decodeError != nil {
		test.Fatal(decodeError)
	}
	if manifest.Name != "tailwindcss" || manifest.Version != TailwindVersion || manifest.License != "MIT" {
		test.Errorf("the snapshot is %s@%s (%s), want tailwindcss@%s (MIT)", manifest.Name, manifest.Version,
			manifest.License, TailwindVersion)
	}
}

// The source directory is found whether runtime.Caller names a path on disk or, built with -trimpath as
// cohere-dev builds everything, an import path, which is resolved under cohere's module root. A runtime.Caller
// path used as it came made the snapshot unavailable to every Tailwind test under -trimpath.
func TestTheSourceDirectoryIsFoundUnderEitherBuild(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.27\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	working := filepath.Join(root, "internal", "lint", "rules", "tailwind")
	if createDirectoryError := os.MkdirAll(working, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	want := filepath.Join(root, "internal", "lint", "rules", "tailwind", "vendored")
	for _, file := range []string{
		filepath.Join(want, "vendored.go"),
		modulePath + "/internal/lint/rules/tailwind/vendored/vendored.go",
	} {
		if got, sourceDirectoryError := sourceDirectory(file, working); sourceDirectoryError != nil || got != want {
			test.Errorf("%s gave %q (%v), want %q", file, got, sourceDirectoryError, want)
		}
	}
	if _, sourceDirectoryError := sourceDirectory(
		"example.com/other/vendored.go",
		working,
	); sourceDirectoryError == nil {
		test.Error("a file outside cohere's module was resolved")
	}
	if _, sourceDirectoryError := sourceDirectory(
		modulePath+"/internal/x.go",
		test.TempDir(),
	); sourceDirectoryError == nil {
		test.Error("a module path was resolved with no go.mod above the working directory")
	}
}

// A -trimpath build names this file by its import path, which is found only from a working directory inside
// cohere's module, so from outside one the snapshot is an error the caller reports, not a panic that stops the
// whole test binary.
// Not parallel: it moves the working directory with test.Chdir
func TestTheSnapshotFromOutsideTheModuleIsAnError(test *testing.T) {
	if _, file, _, _ := runtime.Caller(0); filepath.IsAbs(file) {
		test.Skip("built without -trimpath, the source file is a path on disk, found from any working directory")
	}
	test.Chdir(test.TempDir())
	if root, rootError := TailwindPackageRoot(); rootError == nil {
		test.Errorf("from outside cohere's module the snapshot was found at %s", root)
	}
}
