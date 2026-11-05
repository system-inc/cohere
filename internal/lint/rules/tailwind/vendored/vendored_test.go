package vendored

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The snapshot is the version it says, and holds every file the loader reads, so a refresh that drops one
// fails here rather than as a design system that quietly builds from less.
func TestTheSnapshotIsWholeAndTheVersionItSays(t *testing.T) {
	t.Parallel()
	root := TailwindPackageRoot()
	for _, name := range []string{"index.css", "theme.css", "preflight.css", "utilities.css", "LICENSE", "package.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("the snapshot is missing %s: %v", name, err)
		}
	}
	contents, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		License string `json:"license"`
	}
	if err := json.Unmarshal(contents, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != "tailwindcss" || manifest.Version != TailwindVersion || manifest.License != "MIT" {
		t.Errorf("the snapshot is %s@%s (%s), want tailwindcss@%s (MIT)", manifest.Name, manifest.Version,
			manifest.License, TailwindVersion)
	}
}

// The source directory is found whether runtime.Caller names a path on disk or, built with -trimpath as
// cohere-dev builds everything, an import path, which is resolved under cohere's module root. A runtime.Caller
// path used as it came made the snapshot unavailable to every Tailwind test under -trimpath.
func TestTheSourceDirectoryIsFoundUnderEitherBuild(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+modulePath+"\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(root, "internal", "lint", "rules", "tailwind")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "internal", "lint", "rules", "tailwind", "vendored")
	for _, file := range []string{filepath.Join(want, "vendored.go"), modulePath + "/internal/lint/rules/tailwind/vendored/vendored.go"} {
		if got, err := sourceDirectory(file, working); err != nil || got != want {
			t.Errorf("%s gave %q (%v), want %q", file, got, err, want)
		}
	}
	if _, err := sourceDirectory("example.com/other/vendored.go", working); err == nil {
		t.Error("a file outside cohere's module was resolved")
	}
	if _, err := sourceDirectory(modulePath+"/internal/x.go", t.TempDir()); err == nil {
		t.Error("a module path was resolved with no go.mod above the working directory")
	}
}
