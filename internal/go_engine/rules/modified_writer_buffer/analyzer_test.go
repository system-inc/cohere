package modified_writer_buffer

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
	"honnef.co/go/tools/staticcheck/sa1023"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamPackages are staticcheck's own fixtures for SA1023, copied unchanged from its testdata/go1.0 and
// testdata/go1.9 into testdata/src under directories of the same names. Staticcheck's runner loads each version
// directory as a module of its own, and the two declare the same types, so they stay two packages; the second needs
// type aliases, which the toolchain's own version, newer than both, allows. Their files share one name, so a finding
// names its file by its path under testdata/src.
var upstreamPackages = []string{"go1.0/CheckWriterBufferModified", "go1.9/CheckWriterBufferModified"}

// differencesFile is the differences fixture's one file, by its path under testdata/src.
var differencesFile = filepath.Join("differences", "differences.go")

// underSource is path, a fixture file's full path, as its path under testdata/src.
func underSource(test *testing.T, path string) string {
	test.Helper()
	source, absoluteError := filepath.Abs(filepath.Join(analysistest.TestData(), "src"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	relative, relativeError := filepath.Rel(source, path)
	if relativeError != nil {
		test.Fatalf("placing %s under %s: %v", path, source, relativeError)
	}
	return relative
}

// Staticcheck's own fixtures mark each expected finding with `//@ diag(...)`, read here by the shared reader as
// staticcheck's test runner reads them. Every mark the original expects, the port reports, and any finding no mark
// expects fails as much as a mark nothing found, so the port adds nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	_, port := findingsOf(test, upstreamPackages...)
	source := filepath.Join(analysistest.TestData(), "src")
	wants := []staticcheck_wants.Want{}
	for _, upstreamPackage := range upstreamPackages {
		packageWants, readError := staticcheck_wants.Read(filepath.Join(source, upstreamPackage))
		if readError != nil {
			test.Fatal(readError)
		}
		wants = append(wants, packageWants...)
	}
	test.Logf("the fixtures mark %d, the port found %d", len(wants), len(port))
	if len(wants) == 0 {
		test.Fatal("the upstream fixtures mark nothing, so the comparison proves nothing")
	}
	// A finding names its file by its path under testdata/src, so the wants do too.
	for index := range wants {
		wants[index].File = underSource(test, wants[index].File)
	}
	findings := []staticcheck_wants.Finding{}
	for _, found := range port {
		findings = append(findings, staticcheck_wants.Finding{
			File: found.file, Line: found.line, Message: found.message,
		})
	}
	unexpected, untaken := staticcheck_wants.Compare(wants, findings)
	for _, found := range unexpected {
		test.Errorf("the port found %s:%d: %s, which no mark expects", found.File, found.Line, found.Message)
	}
	for _, want := range untaken {
		test.Errorf("%s, and the port did not find it", want)
	}
}

// The original and the port run in one checker over the same fixture packages, staticcheck's and parity's, the second
// holding the shapes the check depends on, and their findings must be the same multiset of file, line, column, end
// and message. Each side's count is logged, since two empty sides would match and prove nothing, and the test fails if
// the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, append(slices.Clone(upstreamPackages), "parity")...)
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
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture the two may disagree only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is the original's side of a recorded renaming, and every record is matched. A finding the
// original makes and the port does not, unrecorded, fails, since the port is never worse than the original. Today the
// fixture records nothing, because the two read the same form and differ nowhere: it holds the cases where they could
// plausibly disagree, so the test holds that they agree there, and fails if the original finds nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the comparison proves nothing")
	}
	expectedPort := []finding{}
	expectedOriginal := []finding{}
	path := filepath.Join(analysistest.TestData(), "src", differencesFile)
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
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: differencesFile, line: line, message: match[1]})
			default:
				renamed := finding{file: differencesFile, line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, renamed)
				expectedPort = append(expectedPort, finding{file: differencesFile, line: line, message: match[3]})
			}
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
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

// finding is one finding as the differential compares it. Staticcheck's report gives a finding the end of its
// store or call as well as its start, so the end is compared too.
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
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingsOf loads the fixture packages patterns name, runs staticcheck's SA1023 and the port over them in one
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
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa1023.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      underSource(test, position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			switch action.Analyzer {
			case sa1023.Analyzer:
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

// withoutColumns is findings with their columns and ends dropped, for comparing with the differences fixture's
// comments, which name a line.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column = 0
		found.endLine = 0
		found.endColumn = 0
		dropped = append(dropped, found)
	}
	return dropped
}
