package print_of_sprintf

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1038"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// staticcheck's own fixture for S1038, copied unchanged from simple/s1038/testdata/go1.0/CheckPrintSprintf into
// testdata/src/CheckPrintSprintf: every `//@ diag(...)` note the original expects, the port reports on the same line
// with a message the note matches, and the port reports nothing no note expects. The go1.0 folder sets the language
// version of staticcheck's run, which S1038 never reads, so the fixture loads from a tree laid out by import path as
// the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	fixture, absoluteError := filepath.Abs(filepath.Join(analysistest.TestData(), "src", "CheckPrintSprintf"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	wants, readError := staticcheck_wants.Read(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	_, port := parity.Findings(test, s1038.Analyzer, Analyzer, analysistest.TestData(), "CheckPrintSprintf")
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the fixture's files all sit in its one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
		})
	}
	test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// The original and the port run in one checker over staticcheck's fixture and the house's two, parity holding every
// shape the three forms branch on: each print with its arity and the wrong one, the Println forms' literal and named
// formats, parentheses at every level, fmt.Sprintf through a variable, a generic function of the package's own,
// testing's methods on types that embed them, declare their own or hold fields of the same names, and a generated
// file, where the fmt form alone is filtered. dotted imports fmt and log with dots, which a package declaring its
// own Sprintf cannot. Each finding is held whole, its range to its end and its fixes, and S1038 suggests none, so the
// port is held to suggesting none as well.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1038.Analyzer, Analyzer, analysistest.TestData(), "CheckPrintSprintf", "parity",
		"dotted")
}

// The differences fixture holds the shapes where the port could most plausibly part from the original, and records
// none, since the two agree on every one. parity.RequireOnlyAdjudicated refuses a fixture that records nothing, so
// the agreement there is held finding for finding instead, each finding whole, and fails should the original find
// nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1038.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// BenchmarkOverFixtures weighs the original against the port over the four fixtures: each package is loaded once and
// every prerequisite run once on it, so each sub-benchmark times one Run alone, and the allocations it reports are
// the check's and none of what it requires. The port's own analyzer only reads what the dispatcher found, so its side
// times a dispatcher of the benchmark's own that holds the port's listener alone: the walk and the check, as the
// original's Run walks and checks, and the dispatcher's start on each package beside them. It loads the fixtures with
// a loader of its own, since the harness runs a checker and hands back findings, never the passes a benchmark times,
// and it is skipped under -short.
func BenchmarkOverFixtures(benchmark *testing.B) {
	if testing.Short() {
		benchmark.Skip("loads the fixtures")
	}
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"CheckPrintSprintf", "parity", "dotted", "differences"},
		[]string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}, nil)
	if loadError != nil {
		benchmark.Fatal(loadError)
	}
	// A dispatcher names each rule once, and the shared one already holds the port, so the benchmark's is new. The
	// rule's analyzer it returns is never run: the dispatcher's own Run is what calls the listener.
	dispatcher := dispatch.NewSyntax()
	dispatcher.RuleAnalyzer(rule.Namespace+"S1038", "S1038", Analyzer.Doc, &Listener)
	for _, side := range []struct {
		name     string
		analyzer *analysis.Analyzer
	}{{"original", s1038.Analyzer}, {"port", dispatcher.Analyzer()}} {
		passes := []*analysis.Pass{}
		for _, root := range loaded.Roots {
			if len(root.Errors) > 0 {
				benchmark.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
			}
			results := map[*analysis.Analyzer]any{}
			passOf := func(analyzer *analysis.Analyzer) *analysis.Pass {
				return &analysis.Pass{
					Analyzer: analyzer, Fset: root.Fset, Files: root.Syntax, Pkg: root.Types,
					TypesInfo: root.TypesInfo, TypesSizes: root.TypesSizes, ResultOf: results, ReadFile: os.ReadFile,
					Report: func(analysis.Diagnostic) {
						// The fixtures' findings are the tests' to compare; this weighs only what finding them costs.
					},
				}
			}
			// Every prerequisite runs once, in the order its own requirements need, before the analyzer is timed.
			var prepare func(analyzer *analysis.Analyzer)
			prepare = func(analyzer *analysis.Analyzer) {
				for _, required := range analyzer.Requires {
					if _, prepared := results[required]; !prepared {
						prepare(required)
						result, runError := required.Run(passOf(required))
						if runError != nil {
							benchmark.Fatalf("running %s on %s: %v", required.Name, root.ID, runError)
						}
						results[required] = result
					}
				}
			}
			prepare(side.analyzer)
			passes = append(passes, passOf(side.analyzer))
		}
		benchmark.Run(side.name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				for _, pass := range passes {
					if _, runError := side.analyzer.Run(pass); runError != nil {
						benchmark.Fatal(runError)
					}
				}
			}
		})
	}
}
