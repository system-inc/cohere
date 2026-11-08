package printf

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	original "golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
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
// module's go line is 1.27. The harness's RequireUpstreamWants runs without suggested fixes, so it would leave the
// golden files unread, and these keep their own call.
func TestNonConstantFormatStringGo123(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go123.txtar")
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, directory, Analyzer, "example.com/nonconst")
}

func TestNonConstantFormatStringGo124(test *testing.T) {
	test.Parallel()
	directory := extractArchive(test, "nonconst_go124.txtar")
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, directory, Analyzer, "example.com/nonconst")
}

// Over x/tools' fixtures and parity's, the second holding a violation of every message and every branch, a clean
// case beside each, and the shapes most likely to differ, the original and the port find the same findings, each the
// same in every field, and export the same facts.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	patterns := append(slices.Clone(upstreamPackages), "parity", "parity/dependency")
	parity.RequireIdentical(test, original.Analyzer, Analyzer, directory, patterns...)
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	requireSameFacts(test, directory, []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}, patterns)
}

// The two sides agree over the version-gated archives too, each loaded as a module, as analysistest loads it.
func TestPortMatchesOriginalOverTheArchives(test *testing.T) {
	test.Parallel()
	for _, archive := range []string{"nonconst_go123.txtar", "nonconst_go124.txtar"} {
		test.Run(archive, func(test *testing.T) {
			test.Parallel()
			directory := extractArchive(test, archive)
			patterns := []string{"example.com/nonconst"}
			parity.RequireIdentical(test, original.Analyzer, Analyzer, directory, patterns...)
			requireSameFacts(test, directory, []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}, patterns)
		})
	}
}

// requireSameFacts loads the packages patterns name at directory under environment, runs the original and the port
// over them in one checker, and fails the test unless each exports the same facts, each by its object's position,
// the object's name and the fact's words. The harness compares findings and not facts, which printf alone among the
// ports exports, so the facts are compared here on the harness's pieces: its load check, and its Difference over
// each fact written as a Finding at its object. Each side's count is logged, so a run that exports nothing on both
// sides reads as that, never as agreement.
func requireSameFacts(test *testing.T, directory string, environment []string, patterns []string) {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, environment, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// A fixture that does not type-check is skipped by both sides, which would then agree on nothing.
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean, so its facts would go uncounted: %s", failure.Package,
			failure.Error)
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{original.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	exported := map[*analysis.Analyzer][]parity.Finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		objectFacts, _ := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			position := action.Package.Fset.Position(objectFact.Object.Pos())
			exported[action.Analyzer] = append(exported[action.Analyzer], parity.Finding{
				File:    filepath.Base(position.Filename),
				Line:    position.Line,
				Column:  position.Column,
				Message: fmt.Sprintf("%s %v", objectFact.Object.Name(), objectFact.Fact),
			})
		}
	}
	test.Logf("the original exported %d facts, the port %d", len(exported[original.Analyzer]),
		len(exported[Analyzer]))
	for _, missing := range parity.Difference(exported[original.Analyzer], exported[Analyzer]) {
		test.Errorf("the original exported %s, and the port did not", missing)
	}
	for _, extra := range parity.Difference(exported[Analyzer], exported[original.Analyzer]) {
		test.Errorf("the port exported %s, and the original did not", extra)
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

// errorsOnly hands analysistest.RunWithSuggestedFixes a reporter that isn't a testing.TB. Given a testing.TB, it first
// proves the go command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test
// that builds. Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
