package directive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/directive"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// fixturePatterns are the fixture packages, x/tools' package a, and parity's packages, parity, which is neither main
// nor a test, and parity/command, which is main.
var fixturePatterns = []string{"a", "parity", "parity/command"}

// x/tools' own fixtures for directive, byte for byte in testdata/a.txtar, hold the port to every want the original
// expects, and to nothing more. Most of the fixtures are files the build excludes, and one is assembly: analysistest
// reads the wants of a package's other files, and, for an analyzer named directive, of its excluded files too, as
// x/tools' does.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, extractedFixtures(test), "a")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ (testdata/parity.txtar), the original and the port find the same findings, each the same in every field.
// The port differs from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, directive.Analyzer, Analyzer, extractedFixtures(test), fixturePatterns...)
}

// The registry lists directive among the passes that read no type information, so a run may hand it a pass with
// none. The port runs here over the same packages with no types, package or type checker's information at all, and
// must find what the original finds with them. The harness runs every analyzer through the checker, which always
// hands a pass its types, so the untyped pass is built here, and its findings are compared with the original's on
// the harness's pieces: Findings for the original's side and Difference for the comparison.
func TestPortReadsNoTypeInformation(test *testing.T) {
	test.Parallel()
	directory := extractedFixtures(test)
	original, _ := parity.Findings(test, directive.Analyzer, Analyzer, directory, fixturePatterns...)
	loaded, loadError := load.Packages(directory, fixturePatterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", fixturePatterns, loadError)
	}
	untyped := []parity.Finding{}
	for _, loadedPackage := range loaded.Roots {
		pass := &analysis.Pass{
			Analyzer:     Analyzer,
			Fset:         loadedPackage.Fset,
			Files:        loadedPackage.Syntax,
			OtherFiles:   loadedPackage.OtherFiles,
			IgnoredFiles: loadedPackage.IgnoredFiles,
			ReadFile:     os.ReadFile,
			ResultOf:     map[*analysis.Analyzer]any{},
			Report: func(diagnostic analysis.Diagnostic) {
				untyped = append(untyped, untypedFinding(test, directory, loadedPackage, diagnostic))
			},
		}
		if _, runError := Analyzer.Run(pass); runError != nil {
			test.Fatalf("running the port over %s with no types: %v", loadedPackage.ID, runError)
		}
	}
	test.Logf("the original found %d, the port with no types %d", len(original), len(untyped))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range parity.Difference(original, untyped) {
		test.Errorf("the original found %s, and the port with no types did not", missing)
	}
	for _, extra := range parity.Difference(untyped, original) {
		test.Errorf("the port with no types found %s, and the original did not", extra)
	}
}

// untypedFinding is diagnostic, reported in loadedPackage by the port with no types, as the harness sees a finding.
// Its file is named as Findings names the original's, by its path below the fixtures' root, which for this tree laid
// out by import path is directory's src, as `parity/command/main.go`, and a file outside it keeps its path. The
// harness's own conversion is not exported, so this one holds the fields the port's findings have, where each starts
// and ends and its message, and fails the test on a related entry or a fix, which the port never offers and this
// conversion would otherwise drop.
func untypedFinding(
	test *testing.T,
	directory string,
	loadedPackage *packages.Package,
	diagnostic analysis.Diagnostic,
) parity.Finding {
	test.Helper()
	if len(diagnostic.Related) > 0 || len(diagnostic.SuggestedFixes) > 0 {
		test.Errorf("the port with no types reported %q with related entries or fixes, which untypedFinding does "+
			"not compare", diagnostic.Message)
	}
	start := loadedPackage.Fset.Position(diagnostic.Pos)
	file := filepath.ToSlash(start.Filename)
	if below, relativeError := filepath.Rel(filepath.Join(directory, "src"), start.Filename); relativeError == nil &&
		!strings.HasPrefix(below, "..") {
		file = filepath.ToSlash(below)
	}
	found := parity.Finding{
		File:    file,
		Line:    start.Line,
		Column:  start.Column,
		Message: diagnostic.Message,
	}
	if diagnostic.End.IsValid() {
		end := loadedPackage.Fset.Position(diagnostic.End)
		found.EndLine, found.EndColumn = end.Line, end.Column
	}
	return found
}

// fixtureArchives are the fixture trees, each laid out by import path. They are archives because they hold what
// gofmt would change or cannot parse, the odd spaces and broken syntax under test, and cohere's gofmt guard reads
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
