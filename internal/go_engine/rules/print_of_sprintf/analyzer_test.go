package print_of_sprintf

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1038"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
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
// own Sprintf cannot.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1038.Analyzer, Analyzer, analysistest.TestData(), "CheckPrintSprintf", "parity",
		"dotted")
}

// The differences fixture holds the shapes where the port could most plausibly part from the original, and records
// none, since the two agree on every one. parity.RequireOnlyAdjudicated refuses a fixture that records nothing, so
// the agreement there is held finding for finding instead, and fails should the original find nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1038.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The harness compares where a finding starts and what it says. Over every fixture, this compares the rest of each
// finding: where it ends, and each suggested fix's message and edits. S1038 suggests no fix, and the comparison
// holds the port to suggesting none as well.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := wholeFindings(test, "CheckPrintSprintf", "parity", "dotted", "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range difference(port, original) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
}

// wholeFindings loads the fixture packages patterns name, as parity.Findings loads them, runs the original and the
// port in one checker, and returns each one's findings written out whole, sorted: its file by base name, its range
// from start to end, its message, and its suggested fixes, each its message and every edit's range and new text.
func wholeFindings(test *testing.T, patterns ...string) (original []string, port []string) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	for _, root := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{s1038.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			fileSet := action.Package.Fset
			written := []string{fmt.Sprintf("%s-%s: %s", based(fileSet.Position(diagnostic.Pos)),
				based(fileSet.Position(diagnostic.End)), diagnostic.Message)}
			for _, fix := range diagnostic.SuggestedFixes {
				written = append(written, "fix: "+fix.Message)
				for _, edit := range fix.TextEdits {
					written = append(written, fmt.Sprintf("edit %s-%s: %q", based(fileSet.Position(edit.Pos)),
						based(fileSet.Position(edit.End)), edit.NewText))
				}
			}
			switch action.Analyzer {
			case s1038.Analyzer:
				original = append(original, strings.Join(written, "; "))
			case Analyzer:
				port = append(port, strings.Join(written, "; "))
			}
		}
	}
	slices.SortFunc(original, cmp.Compare[string])
	slices.SortFunc(port, cmp.Compare[string])
	return original, port
}

// based is position with its file named by its base name.
func based(position fmt.Stringer) string {
	return filepath.Base(position.String())
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []string, without []string) []string {
	remaining := map[string]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []string{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// BenchmarkOverFixtures weighs the original against the port over the four fixtures: each package is loaded once and
// every prerequisite run once on it, so each sub-benchmark times the analyzer's own Run alone, and the allocations it
// reports are the check's and none of what it requires. It loads the fixtures, so it is skipped under -short.
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
	for _, side := range []struct {
		name     string
		analyzer *analysis.Analyzer
	}{{"original", s1038.Analyzer}, {"port", Analyzer}} {
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
