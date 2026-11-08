package composite

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/composite"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// x/tools' own fixtures for composites, copied unchanged into testdata/src and run as x/tools' test runs them, with
// suggested fixes and no flags: every `// want` the original expects, the port reports at the same line with the
// same message, analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds
// nothing there either, and the port's fixes applied to each file give its .golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and the shapes most likely to differ, and their findings must be the same
// multiset of file, line, column, message and suggested fixes. Each side's count is logged, since two empty sides
// would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	expectSameFindings(test, composite.Analyzer, Analyzer)
}

// The same comparison with the whitelist option set false on both sides, so the allow list's branch is held to the
// original's in both of its settings.
//
// Not parallel: the original keeps its whitelist option in a variable of its package, which every analyzer of it
// reads, so the option is set and put back while no other test of this package runs. Go runs the parallel tests only
// once every sequential one is done.
func TestPortMatchesOriginalWithTheAllowListOff(test *testing.T) {
	if setError := composite.Analyzer.Flags.Set("whitelist", "false"); setError != nil {
		test.Fatalf("turning the original's allow list off: %v", setError)
	}
	test.Cleanup(func() {
		if setError := composite.Analyzer.Flags.Set("whitelist", "true"); setError != nil {
			test.Errorf("turning the original's allow list back on: %v", setError)
		}
	})
	// A dispatcher of its own, since a dispatcher names each rule once and the shared one holds the registry's port.
	port := newAnalyzer(dispatch.NewSyntax())
	if setError := port.Flags.Set("whitelist", "false"); setError != nil {
		test.Fatalf("turning the port's allow list off: %v", setError)
	}
	expectSameFindings(test, composite.Analyzer, port)
}

// expectSameFindings runs original and port over x/tools' fixtures and parity's, and fails on every finding one side
// makes and the other does not.
func expectSameFindings(test *testing.T, original *analysis.Analyzer, port *analysis.Analyzer) {
	test.Helper()
	originalFindings, portFindings := findingsOf(test, original, port, "a", "typeparams", "parity")
	test.Logf("the original found %d, the port %d", len(originalFindings), len(portFindings))
	if len(originalFindings) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(originalFindings, portFindings) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(portFindings, originalFindings) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// finding is one finding as the differential compares it, its suggested fixes included.
type finding struct {
	file    string
	line    int
	column  int
	message string
	fixes   string
}

// described is found as a finding line prints it, followed by its fixes.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d: %s%s", found.file, found.line, found.column, found.message, found.fixes)
}

// findingsOf loads the fixture packages patterns name, runs original and port over them in one checker, and returns
// each one's findings, sorted.
func findingsOf(
	test *testing.T, originalAnalyzer *analysis.Analyzer, portAnalyzer *analysis.Analyzer, patterns ...string,
) (original []finding, port []finding) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// x/tools' fixture a holds a type error on purpose, a literal of a type its import lacks, but parity must
	// type-check, or the checker's guesses at a broken literal would stand in for the shapes it holds.
	for _, root := range loaded.Roots {
		if strings.HasPrefix(root.PkgPath, "parity") && len(root.Errors) > 0 {
			test.Fatalf("parity does not type-check: %v", root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{originalAnalyzer, portAnalyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			found := finding{
				file:    filepath.Base(position.Filename),
				line:    position.Line,
				column:  position.Column,
				message: diagnostic.Message,
				fixes:   describedFixes(action, diagnostic),
			}
			switch action.Analyzer {
			case originalAnalyzer:
				original = append(original, found)
			case portAnalyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
}

// describedFixes is every suggested fix of diagnostic, in order, as its message and each edit's range and new text,
// so two findings match only where they offer the same edits. It is empty for a finding with no fix.
func describedFixes(action *checker.Action, diagnostic analysis.Diagnostic) string {
	var fixes strings.Builder
	for _, fix := range diagnostic.SuggestedFixes {
		fmt.Fprintf(&fixes, " [fix %q:", fix.Message)
		for _, edit := range fix.TextEdits {
			start := action.Package.Fset.Position(edit.Pos)
			end := action.Package.Fset.Position(edit.End)
			fmt.Fprintf(&fixes, " %d:%d-%d:%d %q", start.Line, start.Column, end.Line, end.Column, edit.NewText)
		}
		fixes.WriteString("]")
	}
	return fixes.String()
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

// errorsOnly hands analysistest a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
