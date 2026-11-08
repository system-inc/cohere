package nil_ness

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
	"golang.org/x/tools/go/analysis/passes/nilness"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// portAnalyzer is the port as the registry runs it, a rule analyzer reporting what a dispatcher's walk found, on a
// dispatcher of its own that holds the port alone.
var portAnalyzer = dispatch.RuleAnalyzer(Name, Doc, &Listener, dispatch.NewIr())

// x/tools' own fixtures for nilness, copied unchanged into testdata/src: every `// want` the original expects,
// the port reports at the same line with the same message, and analysistest fails on a finding no want expects as
// much as on a want nothing found, so the port adds nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), portAnalyzer, "a", "b", "c", "d")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding the shapes staticcheck's form builds differently, and their findings must be the same multiset of file,
// line, column and message. Each side's count is logged, since two empty sides would match and prove nothing, and
// the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "a", "b", "c", "d", "parity")
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

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening, which records a
// finding the port makes and the original does not, with its message.
var adjudicatedPattern = regexp.MustCompile(`^finds: (.+?)\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated: every finding the
// port alone makes is on a line whose comment says the port finds it, and every such comment is matched by such a
// finding. The original finds nothing there that the port does not: a finding the port misses is a regression, never
// an adjudicated difference, so the fixture has no way to record one and the test fails on any.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expected := []finding{}
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
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
			if match == nil {
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			}
			expected = append(expected, finding{file: "differences.go", line: line, message: match[1]})
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	if len(expected) == 0 {
		test.Error("the differences fixture records nothing the port finds, so the comparison proves nothing")
	}
	for _, unrecorded := range difference(withoutColumns(difference(port, original)), expected) {
		test.Errorf("the port finds %s, which no comment in the fixture records", described(unrecorded))
	}
	for _, unmatched := range difference(expected, withoutColumns(difference(port, original))) {
		test.Errorf("the fixture records that the port finds %s, and it does not", described(unmatched))
	}
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not, a regression", described(missing))
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

// findingsOf loads the fixture packages patterns name, runs x/tools' nilness and the port over them in one
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
		[]*analysis.Analyzer{nilness.Analyzer, portAnalyzer}, loaded.Roots, &checker.Options{},
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
			case nilness.Analyzer:
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
