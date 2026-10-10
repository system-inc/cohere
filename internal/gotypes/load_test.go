package gotypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture writes a module of one package with no imports, so loading it reads nothing beyond it.
func loadFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "fixture.go")
	if err := os.WriteFile(file, []byte("package fixture\n\nvar Count int = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory, file
}

// An overlay is what the load reads in place of a file: a type error that exists only in the overlay is
// reported, and the file on disk, which has none, is not what was read.
func TestALoadReadsTheOverlayInPlaceOfTheFile(t *testing.T) {
	t.Parallel()

	directory, file := loadFixture(t)
	clean, err := Load(Config{Directory: directory}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Roots) != 1 || len(clean.Roots[0].Errors) > 0 || clean.Roots[0].TypesInfo == nil {
		t.Fatalf("the fixture did not load clean from disk: %+v", clean.Roots)
	}
	if clean.Roots[0].Types.Scope().Lookup("Count") == nil {
		t.Fatal("the fixture's Count was not in its types")
	}

	planted, err := Load(Config{Directory: directory, Overlay: map[string][]byte{file: []byte("package fixture\n\nvar Count int = \"text\"\n")}}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if problems := planted.ErrorsAnywhere(); len(problems) != 1 || !strings.Contains(problems[0].Error(), "fixture.go") {
		t.Fatalf("a type error only the overlay holds was not reported: %v", problems)
	}
}

// go list answers from the disk, so an overlay naming a file no source package holds would be read by
// nothing: it is refused by name rather than ignored.
func TestAnOverlayNoSourcePackageReadsIsRefused(t *testing.T) {
	t.Parallel()

	directory, _ := loadFixture(t)
	added := filepath.Join(directory, "added.go")
	_, err := Load(Config{Directory: directory, Overlay: map[string][]byte{added: []byte("package fixture\n")}}, ".")
	if err == nil || !strings.Contains(err.Error(), added) {
		t.Fatalf("an overlay naming a file the load does not read was not refused by name: %v", err)
	}
}

// writeFiles writes each relative path's contents under directory.
func writeFiles(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A package's tests are checked as go test builds them: the external tests see one p, so a value of p's type
// made by a package that imports p passes to a function only p's in-package test files declare, which it
// could not if that package were checked against p without its tests. And a test function go test would
// refuse to build a main for is named.
func TestTestsAreCheckedAsGoTestBuildsThem(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFiles(t, directory, map[string]string{
		"go.mod":              "module example.com/variants\n\ngo 1.21\n",
		"p/p.go":              "package p\n\ntype T struct{ N int }\n",
		"p/export_test.go":    "package p\n\nfunc Hidden(t T) int { return t.N }\n",
		"p/p_test.go":         "package p_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/variants/p\"\n\t\"example.com/variants/q\"\n)\n\nfunc TestHidden(t *testing.T) { _ = p.Hidden(q.Make()) }\n",
		"q/q.go":              "package q\n\nimport \"example.com/variants/p\"\n\nfunc Make() p.T { return p.T{N: 1} }\n",
		"q/signature_test.go": "package q\n\nfunc TestWrongSignature(count int) {}\n",
	})
	loaded, err := Load(Config{Directory: directory, Tests: true}, "./...")
	if err != nil {
		t.Fatal(err)
	}
	problems := loaded.ErrorsAnywhere()
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "wrong signature for TestWrongSignature") {
		t.Fatalf("want only TestWrongSignature's signature named, got %v", problems)
	}
	variants := map[string]bool{}
	for _, loadedPackage := range loaded.Source {
		variants[loadedPackage.ImportPath] = true
	}
	for _, want := range []string{"example.com/variants/p [example.com/variants/p.test]", "example.com/variants/q [example.com/variants/p.test]", "example.com/variants/p_test [example.com/variants/p.test]"} {
		if !variants[want] {
			t.Errorf("%s was not checked", want)
		}
	}
}
