package testing_goroutine

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/testinggoroutine"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The original's test turns its experimental subtest flag on before anything runs, so the fixtures' subtest wants are
// reported, and these tests do the same, for the original and the port alike.
// TestPortMatchesOriginalWithTheSubtestFlagOff turns both off for its own run, and back on.
func init() {
	setSubtest("true")
}

// setSubtest sets the subtest flag of the original and the port to value.
func setSubtest(value string) {
	for _, analyzer := range []*analysis.Analyzer{testinggoroutine.Analyzer, Analyzer} {
		if setError := analyzer.Flags.Set("subtest", value); setError != nil {
			// Invariant: both declare subtest as a bool flag, which takes "true" and "false".
			panic(setError)
		}
	}
}

// x/tools' own fixtures for testinggoroutine, copied unchanged into testdata/src and run as x/tools' test runs them,
// with the subtest flag on: every `// want` the original expects, the port reports at the same line with the same
// message, and analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds
// nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools', parity's, and
// withouttesting's, the second holding every branch of the judgment and the shapes most likely to differ, and the
// third a package that takes a test without importing testing, and their findings must be the same multiset of file,
// line, column, end and message. Each side's count is logged, since two empty sides would match and prove nothing,
// and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	compareWithOriginal(test)
}

// The same comparison with the subtest flag off, its default and how cohere runs the pass, so the port drops the
// subtest findings the original drops, and keeps every other.
//
// Not parallel: the flag is the analyzers' package state, which every other test here reads on, so this test runs
// alone, before any parallel test resumes, and turns the flag back on before it ends.
func TestPortMatchesOriginalWithTheSubtestFlagOff(test *testing.T) {
	setSubtest("false")
	defer setSubtest("true")
	compareWithOriginal(test)
}

// compareWithOriginal fails test unless the original and the port find the same over every fixture package.
func compareWithOriginal(test *testing.T) {
	test.Helper()
	original, port := findingsOf(test, "a", "typeparams", "parity", "withouttesting")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(port, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, `renames:`, the original's message, `=>` and the port's, for a finding
// both make at one place in words that differ, or `drops:` and the message of a finding only the original makes,
// which is wrong.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?)|drops: (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is recorded on its line, as dropped or as the original's side of a renaming, every record is
// matched, and every record has its reason in a comment on the line above. A finding the original makes and the port
// does not, unrecorded, fails, since the port is never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	content, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	lines := strings.Split(string(content), "\n")
	for index, text := range lines {
		line := index + 1
		comments := strings.Split(text, "// port ")[1:]
		if len(comments) > 0 && (index == 0 || !strings.HasPrefix(strings.TrimSpace(lines[index-1]), "//")) {
			test.Errorf("differences.go:%d records a difference with no reason on the line above", line)
		}
		for _, comment := range comments {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[1]})
			case match[4] != "":
				expectedOriginal = append(
					expectedOriginal, finding{file: "differences.go", line: line, message: match[4]},
				)
			default:
				expectedOriginal = append(
					expectedOriginal, finding{file: "differences.go", line: line, message: match[2]},
				)
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[3]})
			}
		}
	}
	if len(expectedPort)+len(expectedOriginal) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	test.Logf("the fixture records %d findings only the port makes and %d only the original makes",
		len(expectedPort), len(expectedOriginal))
	compareAdjudicated(test, "the port", withoutPlaces(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutPlaces(difference(original, port)), expectedOriginal)
}

// compareAdjudicated fails the test unless the findings side alone makes are exactly those the fixture records.
func compareAdjudicated(test *testing.T, side string, actual []finding, expected []finding) {
	test.Helper()
	for _, unrecorded := range difference(actual, expected) {
		test.Errorf("%s alone found %s, which the fixture does not record", side, described(unrecorded))
	}
	for _, unmatched := range difference(expected, actual) {
		test.Errorf("the fixture records that %s alone finds %s, and it does not", side, described(unmatched))
	}
}

// withoutPlaces is findings with their columns and ends dropped, for comparing with the differences fixture's
// comments, which name a line.
func withoutPlaces(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column = 0
		found.endLine = 0
		found.endColumn = 0
		dropped = append(dropped, found)
	}
	return dropped
}

// finding is one finding as the differential compares it. testinggoroutine offers no fixes, so there are no edits to
// compare.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it, with its end.
func described(found finding) string {
	return fmt.Sprintf(
		"%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn, found.message,
	)
}

// findingsOf loads the fixture packages patterns name, runs x/tools' testinggoroutine and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// The checker passes over a root that does not type-check without a word, so a fixture with one type error would
	// compare nothing and read green. Every root must load clean, and there must be one.
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{testinggoroutine.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			start := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      filepath.Base(start.Filename),
				line:      start.Line,
				column:    start.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			switch action.Analyzer {
			case testinggoroutine.Analyzer:
				original = append(original, found)
			case Analyzer:
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
