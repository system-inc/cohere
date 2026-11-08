package unmarshal

import (
	"bufio"
	"cmp"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	upstream "golang.org/x/tools/go/analysis/passes/unmarshal"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for unmarshal, copied into testdata/src and run as x/tools' test runs them, with no flags:
// every `// want` the original expects, the port reports at the same line with the same message, and analysistest
// fails on a finding no want expects as much as on a want nothing found, so the port adds nothing there either. The
// copies are byte for byte but for typeparams.go, which is gofmt'd: upstream indents one line with spaces and ends
// without a newline, and gofmt changes only that line's indentation and the file's end, so every line stays where it
// was.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "a", "typeparams")
}

// The original and the port run in one checker over the same fixture packages, x/tools', parity's and dotted's, the
// last two holding every branch of the judgment and the shapes most likely to differ, and their findings must be the
// same multiset of file, line, column, end and message. Each side's count is logged, since two empty sides would
// match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "a", "typeparams", "parity", "dotted")
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
// the message of a true finding only the port makes, or `drops:` and the message of a wrong finding only the original
// makes.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|drops: (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line as found, every finding the original alone makes
// is recorded on its line as dropped, with the reason it is wrong on the line above, and every record is matched. A
// finding the original makes and the port does not, unrecorded, fails, since the port is never worse than the
// original.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	scanner := bufio.NewScanner(file)
	previous := ""
	for line := 1; scanner.Scan(); line++ {
		for _, comment := range strings.Split(scanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[1]})
			case !strings.HasPrefix(strings.TrimSpace(previous), "//"):
				test.Fatalf("differences.go:%d drops a finding without its reason on the line above", line)
			default:
				dropped := finding{file: "differences.go", line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, dropped)
			}
		}
		previous = scanner.Text()
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	if len(expectedPort) == 0 && len(expectedOriginal) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	compareAdjudicated(test, "the port", byLine(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", byLine(difference(original, port)), expectedOriginal)
}

// The original leaves the four encoding packages alone, in-package tests included, which share the package's path,
// and judges every other path, their external tests among them. parity's syntax and types are run under each path by
// passes built here, with a package of that path in place of parity's: both analyzers read the package only for its
// path, and the callees they judge from the types are the real encoding packages', as in the encoding packages
// themselves. A path judged must find what parity finds under its own path, on both sides, and a path left alone
// nothing, so the comparison cannot pass on two empty sides.
func TestPortLeavesTheEncodingPackagesAloneAsTheOriginalDoes(test *testing.T) {
	test.Parallel()
	roots := loadedFixtures(test, "parity")
	if len(roots) != 1 {
		test.Fatalf("loading parity found %d packages, expected 1", len(roots))
	}
	parity := roots[0]
	original := findingsUnder(test, upstream.Analyzer, parity, parity.PkgPath)
	port := findingsUnder(test, Analyzer, parity, parity.PkgPath)
	test.Logf("under %s the original found %d, the port %d", parity.PkgPath, len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in parity, so the comparison proves nothing")
	}
	leftAlone := []string{"encoding/gob", "encoding/json", "encoding/xml", "encoding/asn1"}
	judged := []string{
		"encoding/json_test", "encoding/gob_test", "encoding/xml_test", "encoding/asn1_test", "encoding/json.test",
		"encoding", "encoding/base64", "encoding/jsonv2", "example.com/encoding/json",
	}
	for _, path := range slices.Concat(leftAlone, judged) {
		originalUnder := findingsUnder(test, upstream.Analyzer, parity, path)
		portUnder := findingsUnder(test, Analyzer, parity, path)
		expected := original
		if slices.Contains(leftAlone, path) {
			expected = []finding{}
		}
		for side, found := range map[string][]finding{"the original": originalUnder, "the port": portUnder} {
			if len(difference(found, expected)) > 0 || len(difference(expected, found)) > 0 {
				test.Errorf("under %s %s found %d, expected %d", path, side, len(found), len(expected))
			}
		}
	}
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

// finding is one finding as the differential compares it. The original reports at a position with no end, so the
// end is zero on both sides, and comparing it still holds the port to reporting none.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf(
		"%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn, found.message,
	)
}

// loadedFixtures loads the fixture packages patterns name, each of which must load clean.
func loadedFixtures(test *testing.T, patterns ...string) []*packages.Package {
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
	return loaded.Roots
}

// findingsOf loads the fixture packages patterns name, runs x/tools' unmarshal and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{upstream.Analyzer, Analyzer}, loadedFixtures(test, patterns...), &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(action.Package, diagnostic)
			switch action.Analyzer {
			case upstream.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	return sorted(original), sorted(port)
}

// findingsUnder runs analyzer over fixture's syntax and types alone, through passes whose package has path in place
// of fixture's own, and returns its findings, sorted. Everything analyzer requires runs first over the same package,
// as a driver runs it: the original's inspector, and the port's syntax dispatcher with what that walk reads, the
// dispatcher's analyzer itself reading the package's path where the port's listener does.
func findingsUnder(test *testing.T, analyzer *analysis.Analyzer, fixture *packages.Package, path string) []finding {
	test.Helper()
	found := []finding{}
	underPath := types.NewPackage(path, fixture.Types.Name())
	results := map[*analysis.Analyzer]any{}
	var run func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic))
	run = func(running *analysis.Analyzer, report func(diagnostic analysis.Diagnostic)) {
		resultOf := map[*analysis.Analyzer]any{}
		for _, required := range running.Requires {
			if _, isRun := results[required]; !isRun {
				run(required, func(analysis.Diagnostic) {
					// A requirement's findings are not the analyzer's, and none of these makes any.
				})
			}
			resultOf[required] = results[required]
		}
		pass := &analysis.Pass{
			Analyzer:   running,
			Fset:       fixture.Fset,
			Files:      fixture.Syntax,
			Pkg:        underPath,
			TypesInfo:  fixture.TypesInfo,
			TypesSizes: fixture.TypesSizes,
			ResultOf:   resultOf,
			Report:     report,
		}
		result, runError := running.Run(pass)
		if runError != nil {
			test.Fatalf("running %s under %s: %v", running.Name, path, runError)
		}
		results[running] = result
	}
	run(analyzer, func(diagnostic analysis.Diagnostic) {
		found = append(found, findingOf(fixture, diagnostic))
	})
	return sorted(found)
}

// findingOf is diagnostic, reported in fixture, as the differential compares it.
func findingOf(fixture *packages.Package, diagnostic analysis.Diagnostic) finding {
	position := fixture.Fset.Position(diagnostic.Pos)
	end := fixture.Fset.Position(diagnostic.End)
	return finding{
		file:      filepath.Base(position.Filename),
		line:      position.Line,
		column:    position.Column,
		endLine:   end.Line,
		endColumn: end.Column,
		message:   diagnostic.Message,
	}
}

// sorted is findings in the order their described lines sort.
func sorted(findings []finding) []finding {
	slices.SortFunc(findings, func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	})
	return findings
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

// byLine is findings with only their file, line and message kept, for comparing with the differences fixture's
// comments, which name a line.
func byLine(findings []finding) []finding {
	kept := []finding{}
	for _, found := range findings {
		kept = append(kept, finding{file: found.file, line: found.line, message: found.message})
	}
	return kept
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
