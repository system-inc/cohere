package gotypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture writes a module of one package with no imports, so loading it reads nothing beyond it.
func loadFixture(test *testing.T) (string, string) {
	test.Helper()
	directory := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(directory, "go.mod"),
		[]byte("module example.com/fixture\n\ngo 1.21\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	file := filepath.Join(directory, "fixture.go")
	if writeError := os.WriteFile(file, []byte("package fixture\n\nvar Count int = 1\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return directory, file
}

// An overlay is what the load reads in place of a file: a type error that exists only in the overlay is
// reported, and the file on disk, which has none, is not what was read.
func TestALoadReadsTheOverlayInPlaceOfTheFile(test *testing.T) {
	test.Parallel()

	directory, file := loadFixture(test)
	clean, loadError := Load(Config{Directory: directory}, ".")
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(clean.Roots) != 1 || len(clean.Roots[0].Errors) > 0 || clean.Roots[0].TypesInfo == nil {
		test.Fatalf("the fixture did not load clean from disk: %+v", clean.Roots)
	}
	if clean.Roots[0].Types.Scope().Lookup("Count") == nil {
		test.Fatal("the fixture's Count was not in its types")
	}

	planted, loadError := Load(Config{
		Directory: directory,
		Overlay:   map[string][]byte{file: []byte("package fixture\n\nvar Count int = \"text\"\n")},
	}, ".")
	if loadError != nil {
		test.Fatal(loadError)
	}
	if problems := planted.ErrorsAnywhere(); len(problems) != 1 ||
		!strings.Contains(problems[0].Error(), "fixture.go") {
		test.Fatalf("a type error only the overlay holds was not reported: %v", problems)
	}
}

// go list answers from the disk, so an overlay naming a file no source package holds would be read by
// nothing: it is refused by name rather than ignored.
func TestAnOverlayNoSourcePackageReadsIsRefused(test *testing.T) {
	test.Parallel()

	directory, _ := loadFixture(test)
	added := filepath.Join(directory, "added.go")
	_, loadError := Load(
		Config{Directory: directory, Overlay: map[string][]byte{added: []byte("package fixture\n")}},
		".",
	)
	if loadError == nil || !strings.Contains(loadError.Error(), added) {
		test.Fatalf("an overlay naming a file the load does not read was not refused by name: %v", loadError)
	}
}

// writeFiles writes each relative path's contents under directory.
func writeFiles(test *testing.T, directory string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// A package's tests are checked as go test builds them: the external tests see one p, so a value of p's type
// made by a package that imports p passes to a function only p's in-package test files declare, which it
// could not if that package were checked against p without its tests. And a test function go test would
// refuse to build a main for is named.
func TestTestsAreCheckedAsGoTestBuildsThem(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	writeFiles(test, directory, map[string]string{
		"go.mod":              "module example.com/variants\n\ngo 1.21\n",
		"p/p.go":              "package p\n\ntype T struct{ N int }\n",
		"p/export_test.go":    "package p\n\nfunc Hidden(t T) int { return t.N }\n",
		"p/p_test.go":         "package p_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/variants/p\"\n\t\"example.com/variants/q\"\n)\n\nfunc TestHidden(t *testing.T) { _ = p.Hidden(q.Make()) }\n",
		"q/q.go":              "package q\n\nimport \"example.com/variants/p\"\n\nfunc Make() p.T { return p.T{N: 1} }\n",
		"q/signature_test.go": "package q\n\nfunc TestWrongSignature(count int) {}\n",
	})
	loaded, loadError := Load(Config{Directory: directory, Tests: true}, "./...")
	if loadError != nil {
		test.Fatal(loadError)
	}
	problems := loaded.ErrorsAnywhere()
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "wrong signature for TestWrongSignature") {
		test.Fatalf("want only TestWrongSignature's signature named, got %v", problems)
	}
	variants := map[string]bool{}
	for _, loadedPackage := range loaded.Source {
		variants[loadedPackage.ImportPath] = true
	}
	for _, want := range []string{
		"example.com/variants/p [example.com/variants/p.test]",
		"example.com/variants/q [example.com/variants/p.test]",
		"example.com/variants/p_test [example.com/variants/p.test]",
	} {
		if !variants[want] {
			test.Errorf("%s was not checked", want)
		}
	}
}
