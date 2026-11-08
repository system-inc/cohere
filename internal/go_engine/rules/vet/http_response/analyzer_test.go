package http_response

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
	"golang.org/x/tools/go/analysis/passes/httpresponse"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for httpresponse, copied unchanged into testdata/src and run as x/tools' test runs them,
// with no flags: every `// want` the original expects, the port reports at the same line with the same message, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there
// either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools', parity's and indirect's,
// parity holding every branch of the judgment and the shapes most likely to differ, and indirect a package the pass
// skips for not importing net/http, and their findings must be the same multiset of file, line, column and message.
// Each side's count is logged, since two empty sides would match and prove nothing, and the test fails if the
// original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "a", "typeparams", "parity", "indirect")
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
// the message of a finding only the port makes, `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ, or `drops:` and the message of a finding only the original makes, ruled a
// false positive.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?)|drops: (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is recorded as dropped or as the original's side of a renaming, and every record is matched.
// A finding the original makes and the port does not, unrecorded, fails, since the port drops only what was ruled
// false.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	paths, globError := filepath.Glob(filepath.Join(analysistest.TestData(), "src", "differences", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	for _, path := range paths {
		portRecords, originalRecords := adjudicatedIn(test, path)
		expectedPort = append(expectedPort, portRecords...)
		expectedOriginal = append(expectedOriginal, originalRecords...)
	}
	if len(expectedPort) == 0 && len(expectedOriginal) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
}

// adjudicatedIn reads the records of one file of the differences fixture: the findings each says the port alone
// makes, and those it says the original alone makes.
func adjudicatedIn(test *testing.T, path string) (port []finding, original []finding) {
	test.Helper()
	name := filepath.Base(path)
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		for _, comment := range strings.Split(scanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", name, line, comment)
			case match[1] != "":
				port = append(port, finding{file: name, line: line, message: match[1]})
			case match[4] != "":
				original = append(original, finding{file: name, line: line, message: match[4]})
			default:
				original = append(original, finding{file: name, line: line, message: match[2]})
				port = append(port, finding{file: name, line: line, message: match[3]})
			}
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return port, original
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

// findingsOf loads the fixture packages patterns name, runs x/tools' httpresponse and the port over them in one
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
		[]*analysis.Analyzer{httpresponse.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			}
			switch action.Analyzer {
			case httpresponse.Analyzer:
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

// withoutColumns is findings with their columns dropped, for comparing with the differences fixture's comments, which
// name a line.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column = 0
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
