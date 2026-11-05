package load

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// fixtureFiles are the modules the tests load. clean has a package with in-package and external tests, whose
// external tests reach it again through q (so go list builds "q [p.test]"), packages with no tests, and a file
// behind `//go:build ignore`; it requires outside, a module beside it that a pattern names, with an ignored file
// of its own. clean also has directories go list matches as no package: a windows-only package, a directory of
// `//go:build ignore` scripts, and _tools, which ./... skips by its name though go would build its file. Its
// tagged package builds only under the `-tags=special` the load's `GOFLAGS` sets. Two files import "C", which
// cgo being off leaves out though no build line does: one beside plain.go, which go list reports as ignored,
// and one alone in native, which go list matches as no package. broken has one package for each way a load
// fails.
var fixtureFiles = map[string]string{
	"clean/go.mod": "module example.com/clean\n\ngo 1.21\n\nrequire example.com/outside v0.0.0\n\n" +
		"replace example.com/outside => ../outside\n",
	"clean/p/p.go":      "package p\n\nfunc Double(value int) int { return value * 2 }\n",
	"clean/p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestDouble(test *testing.T) { _ = Double(2) }\n",
	"clean/p/external_test.go": "package p_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/clean/p\"\n" +
		"\t\"example.com/clean/q\"\n)\n\nfunc TestQuadruple(test *testing.T) { _ = q.Quadruple(p.Double(1)) }\n",
	"clean/p/ignored.go": "//go:build ignore\n\npackage p\n",
	"clean/q/q.go": "package q\n\nimport \"example.com/clean/p\"\n\n" +
		"func Quadruple(value int) int { return p.Double(p.Double(value)) }\n",
	"clean/plain/plain.go":              "package plain\n\nconst Name = \"plain\"\n",
	"clean/windowsonly/only_windows.go": "package windowsonly\n",
	"clean/plain/native.go":             "package plain\n\nimport \"C\"\n",
	"clean/native/native.go":            "package native\n\nimport \"C\"\n",
	"clean/scripts/first.go":            "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	"clean/scripts/second.go":           "//go:build ignore\n\npackage main\n\nfunc main() {}\n",
	"clean/_tools/tool.go":              "package tools\n",
	"clean/tagged/tagged.go":            "//go:build special\n\npackage tagged\n",

	"outside/go.mod":     "module example.com/outside\n\ngo 1.21\n",
	"outside/outside.go": "package outside\n\nconst Name = \"outside\"\n",
	"outside/ignored.go": "//go:build ignore\n\npackage outside\n",

	"broken/go.mod":         "module example.com/broken\n\ngo 1.21\n",
	"broken/parse/parse.go": "package parse\n\nfunc Broken( {\n}\n",
	"broken/types/types.go": "package types\n\nvar Count int = \"text\"\n",
	"broken/types/types_test.go": "package types\n\nimport \"testing\"\n\n" +
		"func TestCount(test *testing.T) { _ = Count }\n",
	"broken/list/list.go": "package list\n\nimport \"example.com/broken/missing\"\n\n" +
		"var Value = missing.Value\n",
	"broken/signature/signature.go": "package signature\n",
	"broken/signature/signature_test.go": "package signature\n\nimport \"testing\"\n\n" +
		"func TestWrong(count int) {}\n\nfunc BenchmarkWrong(test *testing.T) {}\n",
}

// cleanEnvironment is what clean is loaded under: linux/amd64, so a target that is not the host's shows the
// load's environment reaches every go command it runs, cgo off, and a tag only `GOFLAGS` sets.
var cleanEnvironment = []string{"GOWORK=off", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=-tags=special"}

// fixtures is the modules written and loaded once per test binary: each load type-checks the standard
// library from source, seconds of work every test only reads. census is clean's .go files, as the project's
// census would list them.
var fixtures struct {
	once    sync.Once
	root    string
	census  []string
	clean   *Loaded
	broken  *Loaded
	failure error
}

// TestMain removes the fixtures once every test is done with them.
func TestMain(tests *testing.M) {
	code := tests.Run()
	if fixtures.root != "" {
		// Ignored: a temporary directory left behind fails nothing.
		_ = os.RemoveAll(fixtures.root)
	}
	os.Exit(code)
}

// loadedFixtures writes and loads the fixtures on first use and returns the root they were written under.
func loadedFixtures(test *testing.T) string {
	test.Helper()

	fixtures.once.Do(func() {
		fixtures.root, fixtures.failure = os.MkdirTemp("", "cohere-goengine-load-")
		if fixtures.failure != nil {
			return
		}
		for name, contents := range fixtureFiles {
			path := filepath.Join(fixtures.root, filepath.FromSlash(name))
			if fixtures.failure = os.MkdirAll(filepath.Dir(path), 0o755); fixtures.failure != nil {
				return
			}
			if fixtures.failure = os.WriteFile(path, []byte(contents), 0o644); fixtures.failure != nil {
				return
			}
		}
		fixtures.census, fixtures.failure = goFilesUnder(filepath.Join(fixtures.root, "clean"))
		if fixtures.failure != nil {
			return
		}
		fixtures.clean, fixtures.failure = Packages(filepath.Join(fixtures.root, "clean"),
			[]string{"./...", "example.com/outside"}, cleanEnvironment, fixtures.census)
		if fixtures.failure != nil {
			return
		}
		fixtures.broken, fixtures.failure = Packages(filepath.Join(fixtures.root, "broken"), []string{"./..."},
			[]string{"GOWORK=off"}, nil)
	})
	if fixtures.failure != nil {
		test.Fatalf("loading the fixtures: %v", fixtures.failure)
	}
	return fixtures.root
}

// goFilesUnder is every .go file below directory, absolute.
func goFilesUnder(directory string) ([]string, error) {
	var files []string
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, entryError error) error {
		if entryError != nil {
			return entryError
		}
		if !entry.IsDir() && filepath.Ext(path) == ".go" {
			files = append(files, path)
		}
		return nil
	})
	return files, walkError
}

// identifiers is the ID of each package loaded analyzes, in order.
func identifiers(loaded *Loaded) []string {
	var found []string
	for _, analyzedPackage := range loaded.Packages {
		found = append(found, analyzedPackage.ID)
	}
	return found
}

// The analyzed set is each package with its in-package tests in place of the package alone, its external tests,
// and each package with no tests as itself: never the plain p, never go test's main, never "q [p.test]".
func TestACleanModuleAnalyzesEachVariantOnce(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	analyzedIdentifiers := identifiers(fixtures.clean)
	slices.Sort(analyzedIdentifiers)
	want := []string{
		"example.com/clean/p [example.com/clean/p.test]",
		"example.com/clean/p_test [example.com/clean/p.test]",
		"example.com/clean/plain",
		"example.com/clean/q",
		"example.com/clean/tagged",
		"example.com/outside",
	}
	if !slices.Equal(analyzedIdentifiers, want) {
		test.Fatalf("want the analyzed packages\n%q\ngot\n%q", want, analyzedIdentifiers)
	}
	if len(fixtures.clean.Diagnostics) > 0 {
		test.Fatalf("a clean module loaded with diagnostics: %+v", fixtures.clean.Diagnostics)
	}
	for _, analyzedPackage := range fixtures.clean.Packages {
		if analyzedPackage.Types == nil || analyzedPackage.TypesInfo == nil || analyzedPackage.TypesSizes == nil {
			test.Errorf("%s came back without the types checker.Analyze needs", analyzedPackage.ID)
		}
	}
}

// diagnosticsAt is the diagnostics in the file named relative to the broken module.
func diagnosticsAt(root, relative string) []Diagnostic {
	file := filepath.Join(root, "broken", filepath.FromSlash(relative))
	var found []Diagnostic
	for _, diagnostic := range fixtures.broken.Diagnostics {
		if diagnostic.File == file {
			found = append(found, diagnostic)
		}
	}
	return found
}

// A parse error, a type error and an import of a package that does not exist each come back with their kind,
// the absolute file and the 1-based line and byte column, and the type error, which both "types" and
// "types [types.test]" report, comes back once.
func TestEachLoadErrorIsOneDiagnosticWithItsKindAndPosition(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	for _, expected := range []struct {
		file   string
		kind   Kind
		line   int
		column int
	}{
		{file: "parse/parse.go", kind: KindParse, line: 3, column: 14},
		{file: "types/types.go", kind: KindType, line: 3, column: 17},
		{file: "list/list.go", kind: KindList, line: 3, column: 8},
	} {
		test.Run(expected.file, func(test *testing.T) {
			test.Parallel()

			var matching []Diagnostic
			for _, diagnostic := range diagnosticsAt(root, expected.file) {
				if diagnostic.Kind == expected.kind && diagnostic.Line == expected.line &&
					diagnostic.Column == expected.column && diagnostic.Message != "" {
					matching = append(matching, diagnostic)
				}
			}
			if len(matching) != 1 {
				test.Fatalf("want one %s diagnostic at %d:%d, got %+v", expected.kind, expected.line, expected.column,
					diagnosticsAt(root, expected.file))
			}
		})
	}
	if typeDiagnostics := diagnosticsAt(root, "types/types.go"); len(typeDiagnostics) != 1 {
		test.Errorf("the type error both variants report was not reported once: %+v", typeDiagnostics)
	}
}

// Every wrongly signed test function is a type diagnostic, not only the first, which is all go list names,
// and the first is not named twice.
func TestEveryWronglySignedTestFunctionIsOneTypeDiagnostic(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	var named []string
	for _, diagnostic := range diagnosticsAt(root, "signature/signature_test.go") {
		named = append(named, fmt.Sprintf("%s %d:%d %s", diagnostic.Kind, diagnostic.Line, diagnostic.Column,
			diagnostic.Message))
	}
	want := []string{
		"Type 5:1 wrong signature for TestWrong, must be: func TestWrong(t *testing.T)",
		"Type 7:1 wrong signature for BenchmarkWrong, must be: func BenchmarkWrong(b *testing.B)",
	}
	if !slices.Equal(named, want) {
		test.Fatalf("want\n%q\ngot\n%q", want, named)
	}
}

// excludedInClean is every file of clean the build constraints leave out under cleanEnvironment, as both the
// load and ExcludedByConstraints must name it, with the same reason: p's ignored file, which go list reports,
// the windows-only package and the ignore scripts, which go list matches as no package at all, and the two
// files that import "C", one of each kind, left out because cgo is off.
func excludedInClean(root string) []Excluded {
	constraints := "excluded by build constraints on linux/amd64"
	cgoOff := `imports "C", and cgo is off for linux/amd64`
	var excluded []Excluded
	for _, each := range []struct{ relative, reason string }{
		{"native/native.go", cgoOff},
		{"p/ignored.go", constraints},
		{"plain/native.go", cgoOff},
		{"scripts/first.go", constraints},
		{"scripts/second.go", constraints},
		{"windowsonly/only_windows.go", constraints},
	} {
		excluded = append(excluded, Excluded{
			File:   filepath.Join(root, "clean", filepath.FromSlash(each.relative)),
			Reason: each.reason,
		})
	}
	return excluded
}

// Every file the build constraints leave out is named once, with the target the load read it for: one the
// package and its test variant both list as ignored, and one in a directory go list never reports, found
// from the census. The outside module's own ignored file is not the workspace's to name, and the tagged file
// builds under the load's tags.
func TestFilesTheBuildConstraintsLeftOutAreExcludedOnceForTheLoadsTarget(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	if want := excludedInClean(root); !slices.Equal(fixtures.clean.Excluded, want) {
		test.Fatalf("want\n%+v\ngot\n%+v", want, fixtures.clean.Excluded)
	}
}

// ExcludedByConstraints, which runs before any load, names the same files from the census alone.
func TestExcludedByConstraintsNamesWhatTheLoadExcludes(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	excluded, excludedError := ExcludedByConstraints(filepath.Join(root, "clean"), cleanEnvironment, fixtures.census)
	if excludedError != nil {
		test.Fatal(excludedError)
	}
	if want := excludedInClean(root); !slices.Equal(excluded, want) {
		test.Fatalf("want\n%+v\ngot\n%+v", want, excluded)
	}
}

// A census file go would build that no package holds, as under a directory ./... skips by its name, is unseen:
// nothing type-checked it.
func TestACensusFileNoPackageHoldsThatWouldBuildIsUnseen(test *testing.T) {
	test.Parallel()

	root := loadedFixtures(test)
	if want := []string{filepath.Join(root, "clean", "_tools", "tool.go")}; !slices.Equal(fixtures.clean.Unseen, want) {
		test.Fatalf("want %q unseen, got %q", want, fixtures.clean.Unseen)
	}
}

// The counts are of the whole graph, of the analyzed packages, and of the workspace's files they hold, each
// file once though "p [p.test]" and the plain p both hold p.go.
func TestTheCountsDescribeTheLoad(test *testing.T) {
	test.Parallel()

	loadedFixtures(test)
	if fixtures.clean.PackagesInWorkspace != 6 {
		test.Errorf("want 6 packages in the workspace, got %d", fixtures.clean.PackagesInWorkspace)
	}
	// testing alone reaches dozens of standard packages.
	if fixtures.clean.PackagesLoaded < 20 {
		test.Errorf("want the whole graph counted, got %d packages", fixtures.clean.PackagesLoaded)
	}
	// p.go, p_test.go, external_test.go, q.go, plain.go and tagged.go; outside.go is not the workspace's.
	if fixtures.clean.FilesTypeChecked != 6 {
		test.Errorf("want 6 files type-checked, got %d", fixtures.clean.FilesTypeChecked)
	}
	if fixtures.clean.Elapsed <= 0 {
		test.Errorf("want the elapsed time, got %s", fixtures.clean.Elapsed)
	}
}
