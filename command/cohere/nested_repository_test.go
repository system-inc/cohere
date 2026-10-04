package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/formatfiles"
)

// nestedTree writes a project whose library is a declared submodule, with a submodule declared inside
// the library, the shape of ahra, libraries/structure and libraries/structure/libraries/nexus, and a
// clone under projects/ that nobody declared, as ahra holds six.
func nestedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, directory := range []string{".git", "library/.git", "library/inner/.git", "library/source", "modules", "projects/clone/.git"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTree(t, root, map[string]string{
		"modules/Host.ts":          "export const host   =   1\n",
		"library/source/Thing.ts":  "export const thing   =   1\n",
		"library/source/Tidy.ts":   "export const tidy = 1\n",
		"library/inner/Deep.ts":    "export const deep   =   1\n",
		"library/inner/Settled.ts": "export const settled = 1\n",
		"library/.gitignore":       "ignored/\n",
		"library/ignored/Skip.ts":  "export const skip   =   1\n",
		"projects/clone/Clone.ts":  "export const clone   =   1\n",
		".gitmodules":              "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.gitmodules":      "[submodule \"inner\"]\n\tpath = inner\n\turl = ../inner\n",
	})
	return root
}

// TestARunInsideALibraryWritesTheLibrary: the repository a run writes is the nearest one at or above
// where it started, inside the project; the project itself everywhere else.
func TestARunInsideALibraryWritesTheLibrary(t *testing.T) {
	root := nestedTree(t)
	for _, testCase := range []struct {
		start string
		want  string
	}{
		{".", "."},
		{"modules", "."},
		{"library", "library"},
		{"library/source", "library"},
		{"library/inner", "library/inner"},
	} {
		got := writeRepositoryRoot(filepath.Join(root, testCase.start), root)
		if got != filepath.Join(root, testCase.want) {
			t.Errorf("a run started in %s writes %s, want %s", testCase.start, got, filepath.Join(root, testCase.want))
		}
	}

	// A project that is not a repository at all writes where it always did.
	plain := t.TempDir()
	if err := os.MkdirAll(filepath.Join(plain, "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := writeRepositoryRoot(filepath.Join(plain, "source"), plain); got != plain {
		t.Errorf("a plain directory's run writes %s, want the project %s", got, plain)
	}
}

// TestARunWritesOnlyItsOwnRepository: from inside a library, a file in the project above and a file in
// a repository nested in the library are both somebody else's, named; the library's own file is not.
func TestARunWritesOnlyItsOwnRepository(t *testing.T) {
	root := nestedTree(t)
	library := filepath.Join(root, "library")

	if why := unwritableRepository(library, filepath.Join(library, "source", "Thing.ts")); why != "" {
		t.Errorf("the library's own file was refused: %s", why)
	}
	if why := unwritableRepository(library, filepath.Join(root, "modules", "Host.ts")); why != "the project outside "+library {
		t.Errorf("the project's file was not refused as outside the library: %q", why)
	}
	if why := unwritableRepository(library, filepath.Join(library, "inner", "Deep.ts")); why != "nested repository inner" {
		t.Errorf("a file in a repository nested in the library was not refused under it: %q", why)
	}
	if why := unwritableRepository(root, filepath.Join(library, "source", "Thing.ts")); why != "nested repository library" {
		t.Errorf("from the project, the library's file was not refused under the library: %q", why)
	}
}

// TestTheProjectReadsItsLibrariesDrift: every declared submodule is read, recursively, each with its own
// ignore layers, and each file its own run would rewrite is reported under that repository. A clone
// nobody declared is not read, and nothing is written.
func TestTheProjectReadsItsLibrariesDrift(t *testing.T) {
	root := nestedTree(t)
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (formatfiles.Enumeration, error) {
		return formatfiles.Enumerate(walkRoot, engine.Handles)
	}
	before := treeSnapshot(t, root)

	check, err := checkNestedRepositories(engine, root)
	if err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, drift := range check.Drift {
		relative, _ := filepath.Rel(root, drift.FileName)
		got = append(got, drift.Repository+": "+relative)
	}
	want := []string{"library/inner: library/inner/Deep.ts", "library: library/source/Thing.ts"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		t.Fatalf("drift %v, want %v (the ignored file and the undeclared clone skipped, the project's own file not read here)", got, want)
	}
	if check.Repositories != 2 || check.Files != 4 {
		t.Fatalf("read %d repositories and %d files, want 2 and 4", check.Repositories, check.Files)
	}
	assertTreeUnchanged(t, root, before, "checkNestedRepositories")
}

// TestALibraryIsFormattedFromItsOwnRootAndReadFromItsProject is the ruling end to end, through the
// binary: the project's check reports the library's drift and writes nothing there; a run started
// inside the library formats and fixes the library and writes nothing in the project above it; and the
// project's check is then clean of it.
func TestALibraryIsFormattedFromItsOwnRootAndReadFromItsProject(t *testing.T) {
	binary := buildCohere(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      `{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}`,
		"NexusCohereSettings.json": `{"format":{"ignore":[]}}`,
		// The project's own files: a fixable finding and a formatting one, which no run inside the
		// library may touch.
		"Producer.ts":         "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		"Ugly.ts":             "export const ugly   =   1\n",
		".gitmodules":         "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":        "gitdir: ../.git/modules/library\n",
		"library/Inner.ts":    "export const inner   =   1\n",
		"library/Debugged.ts": "export function debugged(): number {\n  debugger;\n  return 1;\n}\n",
	})
	project := func() map[string]string {
		snapshot := treeSnapshot(t, root)
		for path := range snapshot {
			if strings.HasPrefix(path, filepath.Join(root, "library")+string(filepath.Separator)) {
				delete(snapshot, path)
			}
		}
		return snapshot
	}
	library := filepath.Join(root, "library")

	// The project's check reads the library and reports the file its formatter would rewrite, writing
	// nothing anywhere. The library's fixable finding is lint's to report, at its position, and the run
	// says its fix was not applied.
	before := treeSnapshot(t, root)
	output, code := runCohere(t, binary, root, "--no-fix", "--format")
	if code == 0 || !strings.Contains(output, "nested repositories: 1 read, 2 files, 1 would change under their own run") ||
		!strings.Contains(output, "1 fix not applied: in nested repository library") ||
		!strings.Contains(output, filepath.Join(library, "Inner.ts")+":1:1 - the formatter would rewrite this file in nested repository library") {
		t.Fatalf("the project's check did not report the library's drift, exit %d:\n%s", code, output)
	}
	assertTreeUnchanged(t, root, before, output)

	// A run started inside the library walks the library and writes only there.
	projectBefore := project()
	output, code = runCohere(t, binary, library, "--fix", "--format")
	if code != 0 {
		t.Fatalf("the library's run exited %d:\n%s", code, output)
	}
	if !strings.Contains(output, "walked "+library) {
		t.Fatalf("the library's run did not walk the library:\n%s", output)
	}
	if got := readForTest(t, filepath.Join(library, "Inner.ts")); got != "export const inner = 1;\n" {
		t.Fatalf("the library's run did not format the library:\n%s\noutput:\n%s", got, output)
	}
	if got := readForTest(t, filepath.Join(library, "Debugged.ts")); strings.Contains(got, "debugger") {
		t.Fatalf("the library's run did not fix the library:\n%s", got)
	}
	projectAfter := project()
	for path, contents := range projectBefore {
		if projectAfter[path] != contents {
			t.Errorf("the library's run wrote %s in the project above it:\n%s", path, projectAfter[path])
		}
	}
	for path := range projectAfter {
		if _, existed := projectBefore[path]; !existed {
			t.Errorf("the library's run created %s in the project above it", path)
		}
	}

	// The project's check now reads the library clean, and its own files are still its own to fix.
	output, _ = runCohere(t, binary, root, "--no-fix", "--format")
	if !strings.Contains(output, "nested repositories: 1 read, 2 files, 0 would change under their own run") {
		t.Fatalf("the library is still reported after its own run formatted it:\n%s", output)
	}
	if !strings.Contains(output, filepath.Join(root, "Producer.ts")+":1:1 - --fix would rewrite this file") {
		t.Fatalf("the project's own drift went missing:\n%s", output)
	}
}
