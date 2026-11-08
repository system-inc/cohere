package directive

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for directive, byte for byte in testdata/a.txtar, run as x/tools' test runs them, with no
// flags: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either. Most of the fixtures are files the build excludes, and one is assembly: analysistest reads the wants of a
// package's other files, and, for an analyzer named directive, of its excluded files too, as x/tools' does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, extractedFixtures(test), Analyzer, "a")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ (testdata/parity.txtar), and their
// findings must be the same multiset of file, line, column and message. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, fixtureRoots(test))
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectSame(test, original, port)
}

// The registry lists directive among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over the same packages with no types, package or type checker's information at all, and
// must find what the original finds with them.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	roots := fixtureRoots(test)
	original, _ := findingsOf(test, roots)
	untyped := []finding{}
	for _, loadedPackage := range roots {
		pass := &analysis.Pass{
			Analyzer:     Analyzer,
			Fset:         loadedPackage.Fset,
			Files:        loadedPackage.Syntax,
			OtherFiles:   loadedPackage.OtherFiles,
			IgnoredFiles: loadedPackage.IgnoredFiles,
			ReadFile:     os.ReadFile,
			ResultOf:     map[*analysis.Analyzer]any{},
			Report: func(diagnostic analysis.Diagnostic) {
				untyped = append(untyped, findingAt(loadedPackage, diagnostic))
			},
		}
		if _, runError := Analyzer.Run(pass); runError != nil {
			test.Fatalf("running the port over %s with no types: %v", loadedPackage.ID, runError)
		}
	}
	slices.SortFunc(untyped, compareFindings)
	test.Logf("the original found %d, the port with no types %d", len(original), len(untyped))
	expectSame(test, original, untyped)
}

// finding is one finding as the differential compares it.
type finding struct {
	file    string
	line    int
	column  int
	message string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d: %s", found.file, found.line, found.column, found.message)
}

// compareFindings orders findings by the line each prints.
func compareFindings(first finding, second finding) int {
	return cmp.Compare(described(first), described(second))
}

// findingAt is diagnostic, reported in loadedPackage, as the differential compares it.
func findingAt(loadedPackage *packages.Package, diagnostic analysis.Diagnostic) finding {
	position := loadedPackage.Fset.Position(diagnostic.Pos)
	return finding{
		file:    filepath.Base(position.Filename),
		line:    position.Line,
		column:  position.Column,
		message: diagnostic.Message,
	}
}

// expectSame fails unless original and other are the same multiset of findings, and the original found any.
func expectSame(test *testing.T, original []finding, other []finding) {
	test.Helper()
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, other) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(other, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// fixtureArchives are the fixture trees, each laid out by import path: x/tools' package a, and parity's packages,
// parity, which is neither main nor a test, and parity/command, which is main. They are archives because they hold
// what gofmt would change or cannot parse, the odd spaces and broken syntax under test, and cohere's gofmt guard reads
// every Go file under testdata.
var fixtureArchives = []string{"a.txtar", "parity.txtar"}

// extractedFixtures extracts every fixture archive into one temporary tree, a `GOPATH`, and returns its root.
func extractedFixtures(test *testing.T) string {
	test.Helper()
	tree := test.TempDir()
	for _, archiveName := range fixtureArchives {
		archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), archiveName))
		if parseError != nil {
			test.Fatalf("reading %s: %v", archiveName, parseError)
		}
		for _, file := range archive.Files {
			path := filepath.Join(tree, filepath.FromSlash(file.Name))
			if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
				test.Fatal(makeError)
			}
			if writeError := os.WriteFile(path, file.Data, 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
	}
	// An archive ends every file it holds with a newline, so the one fixture whose last line has none, which the
	// reader of a non-Go file cuts as it cuts every other, is written here.
	noFinalNewline := filepath.Join(tree, "src", "parity", "command", "no_newline.s")
	if writeError := os.WriteFile(noFinalNewline, []byte("//go:debug panicnil=1"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return tree
}

// fixtureRoots loads every fixture package from the extracted tree, each with its test variants, as analysistest
// loads one.
func fixtureRoots(test *testing.T) []*packages.Package {
	test.Helper()
	directory := extractedFixtures(test)
	patterns := []string{"a", "parity", "parity/command"}
	loadedPackages, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	return loadedPackages.Roots
}

// findingsOf runs x/tools' directive and the port over roots in one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, roots []*packages.Package) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{directive.Analyzer, Analyzer}, roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %d packages: %v", len(roots), analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingAt(action.Package, diagnostic)
			switch action.Analyzer {
			case directive.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	slices.SortFunc(original, compareFindings)
	slices.SortFunc(port, compareFindings)
	return original, port
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []finding, without []finding) []finding {
	remaining := map[finding]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []finding{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
