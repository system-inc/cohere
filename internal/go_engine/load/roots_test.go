package load

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/packages"
)

// rootIdentifiers is the ID of each of loaded's Roots, sorted.
func rootIdentifiers(loaded *Loaded) []string {
	var found []string
	for _, root := range loaded.Roots {
		found = append(found, root.ID)
	}
	slices.Sort(found)
	return found
}

// The roots are what go/packages returns with Tests set, less go test's main: the plain p beside "p [p.test]" and
// "p_test [p.test]", and each package with no tests, while "q [p.test]", which only p's external tests reach, is
// no root.
func TestTheRootsAreEachMatchedPackageAndItsTestVariants(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	want := []string{
		"example.com/clean/p",
		"example.com/clean/p [example.com/clean/p.test]",
		"example.com/clean/p_test [example.com/clean/p.test]",
		"example.com/clean/plain",
		"example.com/clean/q",
		"example.com/clean/tagged",
		"example.com/outside",
	}
	if found := rootIdentifiers(fixtures.clean); !slices.Equal(found, want) {
		test.Fatalf("want the roots\n%q\ngot\n%q", want, found)
	}
}

// gopathFiles are a `GOPATH` tree, as analysistest's `GOPATH` mode loads one: p with in-package and external
// tests, whose external tests reach p again through q.
var gopathFiles = map[string]string{
	"src/p/p.go":      "package p\n\nfunc Double(value int) int { return value * 2 }\n",
	"src/p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestDouble(test *testing.T) { _ = Double(2) }\n",
	"src/p/external_test.go": "package p_test\n\nimport (\n\t\"testing\"\n\n\t\"p\"\n\t\"q\"\n)\n\n" +
		"func TestQuadruple(test *testing.T) { _ = q.Quadruple(p.Double(1)) }\n",
	"src/q/q.go": "package q\n\nimport \"p\"\n\nfunc Quadruple(value int) int { return p.Double(p.Double(value)) }\n",
}

// A `GOPATH` tree loads in `GOPATH` mode, under the environment analysistest gives it, as go/packages loaded it: p,
// its two test variants, each type-checked without an error and in no module, with p's files under src, and the
// external tests reaching "q [p.test]", built against "p [p.test]".
func TestAGopathTreeLoadsInGopathMode(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range gopathFiles {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	loaded, loadError := Packages(directory, []string{"p"},
		[]string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	want := []string{"p", "p [p.test]", "p_test [p.test]"}
	if found := rootIdentifiers(loaded); !slices.Equal(found, want) {
		test.Fatalf("want the roots\n%q\ngot\n%q", want, found)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 || root.Types == nil || len(root.Syntax) == 0 || root.Module != nil {
			test.Errorf("want %s type-checked in no module and without errors, got errors %+v, module %+v",
				root.ID, root.Errors, root.Module)
		}
		if root.Dir != filepath.Join(directory, "src", "p") {
			test.Errorf("want %s in %s, got %s", root.ID, filepath.Join(directory, "src", "p"), root.Dir)
		}
	}
	external := slices.IndexFunc(loaded.Roots, func(root *packages.Package) bool {
		return root.ID == "p_test [p.test]"
	})
	if imported := loaded.Roots[external].Imports["q"]; imported == nil || imported.ID != "q [p.test]" {
		test.Errorf("want p's external tests to import q [p.test], got %+v", loaded.Roots[external].Imports)
	}
}

// go test's synthesized main, the one root that is no test variant and imports one, is left out of what
// go/packages returned; the package it tests and its variants stay.
func TestWithoutTestMainDropsOnlyTheSynthesizedMain(test *testing.T) {
	test.Parallel()

	plain := &packages.Package{ID: "p"}
	withTests := &packages.Package{ID: "p [p.test]", ForTest: "p"}
	testMain := &packages.Package{ID: "p.test", Imports: map[string]*packages.Package{"p": withTests}}
	kept := withoutTestMain([]*packages.Package{plain, withTests, testMain})
	if !slices.Equal(kept, []*packages.Package{plain, withTests}) {
		test.Fatalf("want p and p [p.test] kept, got %+v", kept)
	}
}
