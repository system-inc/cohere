package tests

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/tests"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstreamPackages are the fixture packages x/tools' own test runs, copied unchanged (byte for byte) into
// testdata/src with b, which b_x_test imports. a is loaded with its test variants, a_test among them, which looks up
// what its examples name through its import of a.
var upstreamPackages = []string{"a", "b_x_test", "divergent", "typeparams"}

// x/tools' own fixtures for tests, run as x/tools' test runs them, with no flags: every `// want` the original
// expects, the port reports at the same line with the same message, and analysistest fails on a finding no want
// expects as much as on a want nothing found, so the port adds nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, upstreamPackages...)
}

// The original and the port run in one checker over the same fixture packages, x/tools', parity's and dotted's, the
// last two holding every branch of the judgment and the shapes most likely to differ (parity's external test package
// among them, and dotted's test importing testing with a dot, whose names would clash with parity's), and their
// findings must be the same multiset of file, line, column, end and message. Each side's count is logged, since two
// empty sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, append(slices.Clone(upstreamPackages), "parity", "dotted")...)
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
// the message of a finding only the port makes, `drops:` and the message of a finding only the original makes, which
// is false, or `renames:`, the original's message, `=>` and the port's, for a finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|drops: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is recorded on its line as dropped, with the reason it is false on the line above, or is the
// original's side of a renaming, and every record is matched. A finding the original makes and the port does not,
// unrecorded, fails, since the port is never worse than the original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	directory := filepath.Join(analysistest.TestData(), "src", "differences")
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		file := filepath.ToSlash(filepath.Join("differences", entry.Name()))
		lines := linesOf(test, filepath.Join(directory, entry.Name()))
		for index, text := range lines {
			line := index + 1
			for _, comment := range strings.Split(text, "// port ")[1:] {
				match := adjudicatedPattern.FindStringSubmatch(comment)
				switch {
				case match == nil:
					test.Fatalf("%s:%d holds a comment the test cannot read: %q", file, line, comment)
				case match[1] != "":
					expectedPort = append(expectedPort, finding{file: file, line: line, message: match[1]})
				case match[2] != "":
					// A drop says why the original's finding is false, in a comment on the line above.
					if index == 0 || !strings.HasPrefix(strings.TrimSpace(lines[index-1]), "//") {
						test.Errorf("%s:%d drops a finding with no reason on the line above", file, line)
					}
					expectedOriginal = append(expectedOriginal, finding{file: file, line: line, message: match[2]})
				default:
					expectedOriginal = append(expectedOriginal, finding{file: file, line: line, message: match[3]})
					expectedPort = append(expectedPort, finding{file: file, line: line, message: match[4]})
				}
			}
		}
	}
	if len(expectedPort)+len(expectedOriginal) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	compareAdjudicated(test, "the port", withoutPlacement(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutPlacement(difference(original, port)), expectedOriginal)
}

// linesOf is the lines of the file at path.
func linesOf(test *testing.T, path string) []string {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return lines
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

// finding is one finding as the differential compares it. file is the path under testdata/src, so two fixture
// packages' files of one name stay apart, and the end is zero where a finding marks a position and no range.
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

// findingsOf loads the fixture packages patterns name, with their test variants, runs x/tools' tests and the port
// over them in one checker, and returns each one's findings, sorted.
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
		[]*analysis.Analyzer{tests.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	sources := filepath.Join(directory, "src")
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			file, relativeError := filepath.Rel(sources, position.Filename)
			if relativeError != nil {
				test.Fatalf("placing %s under %s: %v", position.Filename, sources, relativeError)
			}
			found := finding{
				file:      filepath.ToSlash(file),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			switch action.Analyzer {
			case tests.Analyzer:
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

// withoutPlacement is findings with their columns and ends dropped, for comparing with the differences fixture's
// comments, which name a line.
func withoutPlacement(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column, found.endLine, found.endColumn = 0, 0, 0
		dropped = append(dropped, found)
	}
	return dropped
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
