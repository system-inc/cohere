package typed_nil_interface_comparison

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
	"honnef.co/go/tools/staticcheck/sa4023"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamPackages are staticcheck's own fixtures for SA4023, copied unchanged from its testdata into testdata/src,
// each under its Go version folder, which stays the first element of its import path since three of them are packages
// named CheckTypedNilInterface. SA4023 reads no Go version, so the fixtures load from one tree laid out by import path.
var upstreamPackages = []string{
	"go1.0/CheckTypedNilInterface",
	"go1.0/i26000",
	"go1.0/i27815",
	"go1.0/i28241",
	"go1.0/i31873",
	"go1.0/i33965",
	"go1.0/i33994",
	"go1.0/i35217",
	"go1.9/CheckTypedNilInterface",
	"go1.18/CheckTypedNilInterface",
}

// Staticcheck's own fixtures, read through the shared staticcheck_wants reader: every finding their `//@ diag` notes
// want, the port reports on that line with a message the note matches, and any finding no note wants fails as much
// as a want nothing found, so the port adds nothing there either.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	source, absoluteError := filepath.Abs(filepath.Join(analysistest.TestData(), "src"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	wants := []staticcheck_wants.Want{}
	for _, upstreamPackage := range upstreamPackages {
		packageWants, readError := staticcheck_wants.Read(filepath.Join(source, filepath.FromSlash(upstreamPackage)))
		if readError != nil {
			test.Fatal(readError)
		}
		wants = append(wants, packageWants...)
	}
	_, port := findingsOf(test, upstreamPackages...)
	test.Logf("the fixtures want %d, the port found %d", len(wants), len(port))
	if len(wants) == 0 {
		test.Fatal("the upstream fixtures want nothing, so the comparison proves nothing")
	}
	findings := []staticcheck_wants.Finding{}
	for _, found := range port {
		findings = append(findings, staticcheck_wants.Finding{
			File:    filepath.Join(source, filepath.FromSlash(found.file)),
			Line:    found.line,
			Message: found.message,
		})
	}
	unexpected, untaken := staticcheck_wants.Compare(wants, findings)
	for _, found := range unexpected {
		test.Errorf("the port found %s:%d: %s, which no want expects", found.File, found.Line, found.Message)
	}
	for _, want := range untaken {
		test.Errorf("%s, and the port did not find it", want)
	}
}

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding results
// of functions in its own package and in parity/producer, whose nilness facts cross the package boundary, by every
// position up to the twelfth, methods, function values, closures and interface methods, interface values made from
// pointers, numbers, constants and errors, == and !=, nil on the left, switches, phis, generic code, a test file
// and a generated file, and their findings must be the same multiset of file, line, column, end, message and related
// places, each with its file. Each side's count is logged, since two empty sides would match and prove nothing, and
// the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, append(slices.Clone(upstreamPackages), "parity", "parity/producer")...)
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
	fixture := "differences/differences.go"
	file, openError := os.Open(filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(fixture)))
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", fixture, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: fixture, line: line, message: match[1]})
			default:
				expectedOriginal = append(expectedOriginal, finding{file: fixture, line: line, message: match[2]})
				expectedPort = append(expectedPort, finding{file: fixture, line: line, message: match[3]})
			}
		}
	}
	if scanError := fileScanner.Err(); scanError != nil {
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
// comparison as well as its start, and the places related to it, so the end and the related places are compared too.
// Files are named by their path under testdata/src, since two upstream fixtures share a file name.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
	// related is each related place, its file, start, end and message, in the order the finding gives them.
	related string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s%s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message, found.related)
}

// findingsOf loads the fixture packages patterns name, runs staticcheck's SA4023 and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	directory := analysistest.TestData()
	source, absoluteError := filepath.Abs(filepath.Join(directory, "src"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, loadedPackage := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.ID, loadedPackage.Errors)
		}
	}
	// The packages, not the roots: a package with tests is analyzed once, as its test variant, which holds all its
	// files, as staticcheck's runner analyzes it, where the roots hold both and would report its files twice.
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa4023.Analyzer, Analyzer}, loaded.Packages, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	// relative is a file's path under testdata/src, or its base name for a file outside it.
	relative := func(path string) string {
		under, relativeError := filepath.Rel(source, path)
		if relativeError != nil || strings.HasPrefix(under, "..") {
			return filepath.Base(path)
		}
		return filepath.ToSlash(under)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      relative(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}
			for _, place := range diagnostic.Related {
				start := action.Package.Fset.Position(place.Pos)
				placeEnd := action.Package.Fset.Position(place.End)
				found.related += fmt.Sprintf(" [%s:%d:%d-%d:%d: %s]", relative(start.Filename), start.Line,
					start.Column, placeEnd.Line, placeEnd.Column, place.Message)
			}
			switch action.Analyzer {
			case sa4023.Analyzer:
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

// withoutColumns is findings with their columns, ends and related places dropped, for comparing with the differences
// fixture's comments, which name a line and a message.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		found.column = 0
		found.endLine = 0
		found.endColumn = 0
		found.related = ""
		dropped = append(dropped, found)
	}
	return dropped
}
