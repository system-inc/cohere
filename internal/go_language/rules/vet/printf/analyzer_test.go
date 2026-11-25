package printf

import (
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	original "golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/parity"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// upstreamPackages are the fixture packages x/tools' own test runs the original over, copied unchanged into
// testdata/src.
var upstreamPackages = []string{
	"a", "b", "nofmt", "nonconst", "typeparams", "issue62595", "issue68744", "issue70572", "issue72850", "issue76616",
	"w",
}

// The original's test sets its -funcs flag to "Warn,Warnf" on its one global analyzer, and so does this package's
// test, on both analyzers, here, before any test runs: each flag adds to a package-level table every run reads, so
// setting it inside a parallel test would race the others reading it.
func init() {
	for _, analyzer := range []*analysis.Analyzer{original.Analyzer, Analyzer} {
		if setError := analyzer.Flags.Set("funcs", "Warn,Warnf"); setError != nil {
			panic("two names, neither empty, always set: " + setError.Error())
		}
	}
}

// x/tools' own fixtures for printf, copied unchanged into testdata/src, hold the port to every want the original
// expects, findings and facts, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// The original's two version-gated tests, each a module of its own in a txtar archive: a file of go1.24 or later
// gets the non-constant format finding and its suggested fix, which must give the archive's golden file, and an
// earlier file gets neither. The original extracts them with x/tools' internal testfiles and skips with its internal
// testenv on a Go before 1.23 or 1.24; here the public txtar does the extracting, and no skip is needed, since this
// module's go line is 1.27. The original runs them with suggested fixes, so they run through the harness's
// RequireUpstreamWantsWithFixes, which reads the golden files, over the extracted module.
func TestNonConstantFormatStringGo123(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go123.txtar")
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, directory, "example.com/nonconst")
}

func TestNonConstantFormatStringGo124(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go124.txtar")
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, directory, "example.com/nonconst")
}

// Over x/tools' fixtures and parity's, the second holding a violation of every message and every branch, a clean
// case beside each, and the shapes most likely to differ, the original and the port find the same findings, each the
// same in every field, and export the same facts.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	patterns := append(slices.Clone(upstreamPackages), "parity", "parity/dependency")
	parity.RequireIdentical(test, original.Analyzer, Analyzer, directory, patterns...)
	parity.RequireIdenticalFacts(test, original.Analyzer, Analyzer, directory, patterns...)
}

// The two sides agree over the version-gated archives too, each loaded as a module, as analysistest loads it. Their
// facts are not compared: each archive's functions are all named _, so neither side exports a fact there, and
// RequireIdenticalFacts fails a comparison of two empty sides as proving nothing.
func TestPortMatchesOriginalOverTheArchives(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"nonconst_go123.txtar", "nonconst_go124.txtar"} {
		test.Run(archive, func(test *testing.T) {
			test.Parallel()
			directory := extractArchive(test, archive)
			parity.RequireIdentical(test, original.Analyzer, Analyzer, directory, "example.com/nonconst")
		})
	}
}

// extractArchive writes the txtar archive of testdata named name into a directory of the test's own, as x/tools'
// testfiles.ExtractTxtarFileToTmp does, and returns the directory.
func extractArchive(test *testing.T, name string) string {
	test.Helper()
	archive, parseError := txtar.ParseFile(filepath.Join(analysistest.TestData(), name))
	if parseError != nil {
		test.Fatal(parseError)
	}
	files, filesError := txtar.FS(archive)
	if filesError != nil {
		test.Fatal(filesError)
	}
	directory := test.TempDir()
	if copyError := os.CopyFS(directory, files); copyError != nil {
		test.Fatal(copyError)
	}
	return directory
}

// BenchmarkOverTheCheckPackage weighs the port over the engine's check package, a real package of the repository
// and one of its largest, whose many fmt calls with constant formats are parsed operation by operation. The package
// is loaded once and inspect run once on it, so each iteration is the port's run alone, and the allocations reported
// are the port's. The facts of the package's dependencies are never computed here, so every import of one misses,
// which finds no wrapper outside the package and leaves every call of fmt and the rest of the known names checked as
// the engine checks them. It is skipped under -short.
func BenchmarkOverTheCheckPackage(benchmark *testing.B) {
	if testing.Short() {
		benchmark.Skip("loads a package of the repository")
	}
	loaded, loadError := load.Packages(
		".",
		[]string{"github.com/system-inc/cohere/internal/go_language/check"},
		nil,
		nil,
	)
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	passes := []*analysis.Pass{}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			benchmark.Fatalf("package %s does not load clean: %v", root.ID, root.Errors)
		}
		results := map[*analysis.Analyzer]any{}
		passOf := func(analyzer *analysis.Analyzer) *analysis.Pass {
			return &analysis.Pass{
				Analyzer: analyzer, Fset: root.Fset, Files: root.Syntax, Pkg: root.Types, TypesInfo: root.TypesInfo,
				TypesSizes: root.TypesSizes, ResultOf: results, ReadFile: os.ReadFile,
				Report: func(analysis.Diagnostic) {
					// The findings are the tests' to compare; this weighs only what finding them costs.
				},
				ImportObjectFact: func(types.Object, analysis.Fact) bool { return false },
				ExportObjectFact: func(types.Object, analysis.Fact) {
					// The facts are the tests' to compare; the next iteration finds the same wrappers anew.
				},
			}
		}
		for _, required := range Analyzer.Requires {
			result, runError := required.Run(passOf(required))
			if runError != nil {
				benchmark.Fatalf("running %s on %s: %v", required.Name, root.ID, runError)
			}
			results[required] = result
		}
		passes = append(passes, passOf(Analyzer))
	}
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		for _, pass := range passes {
			if _, runError := Analyzer.Run(pass); runError != nil {
				benchmark.Fatal(runError)
			}
		}
	}
}
