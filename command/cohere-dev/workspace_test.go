//go:build unix

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The modules a run covers are go.work's uses, minus any inside a submodule, found by its gitlink in the
// index rather than by its name; a nested module's pattern is its module path, which reaches it through the
// workspace from the root.
func TestReadWorkspaceLeavesOutAModuleInsideASubmodule(t *testing.T) {
	t.Parallel()
	fixture := newLandFixture(t)
	library := t.TempDir()
	fixture.git(library, "init", "-q", "-b", "main")
	fixture.commit(library, "first.txt")
	files := map[string]string{
		"go.work":      "go 1.21\n\nuse (\n\t.\n\t./inner\n\t./library/mod\n)\n",
		"go.mod":       "module example.com/root\n\ngo 1.21\n",
		"inner/go.mod": "module example.com/root/inner\n\ngo 1.21\n",
	}
	for name, contents := range files {
		path := filepath.Join(fixture.repository, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fixture.git(fixture.repository, "add", ".")
	fixture.git(fixture.repository, "-c", "protocol.file.allow=always", "submodule", "add", "-q", library, "library")
	fixture.git(fixture.repository, "commit", "-q", "-m", "a workspace")

	covered, err := readWorkspace(fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	if got := covered.patterns(); !slices.Equal(got, []string{"./...", "example.com/root/inner/..."}) {
		t.Errorf("patterns are %q, want the root's and inner's", got)
	}
	if !slices.Equal(covered.inSubmodule, []string{"library/mod"}) {
		t.Errorf("left to a submodule: %q, want library/mod", covered.inSubmodule)
	}
}
