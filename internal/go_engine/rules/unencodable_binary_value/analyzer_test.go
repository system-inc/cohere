package unencodable_binary_value

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
	"honnef.co/go/tools/staticcheck/sa1003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// upstreamModules are staticcheck's own fixtures for SA1003, one for each Go version its run sets, copied unchanged
// from staticcheck/sa1003/testdata/<version>/CheckEncodingBinary into testdata/<version>/CheckEncodingBinary. Each
// version's folder holds a go.mod naming that version, the one staticcheck's testutil lays over the folder as it runs
// it, since the version decides whether a bool is encodable: Go 1.7's standard library could not encode one, and Go
// 1.8's can.
var upstreamModules = []string{"go1.0", "go1.7", "go1.8"}

// Every `//@ diag(...)` note the original expects in staticcheck's fixtures, the port reports on the same line with a
// message the note matches, and the port reports nothing no note expects, as staticcheck_wants matches them, each
// version's fixture loaded as the module of its version.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wanted := 0
	for _, module := range upstreamModules {
		directory := filepath.Join(analysistest.TestData(), module)
		wants, readError := staticcheck_wants.Read(directory)
		if readError != nil {
			test.Fatal(readError)
		}
		wanted += len(wants)
		graph := analyze(test, []*analysis.Analyzer{Analyzer}, module)
		findings := []staticcheck_wants.Finding{}
		for _, action := range graph.Roots {
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				position := action.Package.Fset.Position(diagnostic.Pos)
				findings = append(findings, staticcheck_wants.Finding{
					File:    position.Filename,
					Line:    position.Line,
					Message: diagnostic.Message,
				})
			}
		}
		test.Logf("%s: the fixture wants %d, the port reported %d", module, len(wants), len(findings))
		unexpected, untaken := staticcheck_wants.Compare(wants, findings)
		for _, found := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", found.File, found.Line,
				found.Message)
		}
		for _, want := range untaken {
			test.Errorf("the port reported nothing matching %s", want)
		}
	}
	if wanted == 0 {
		test.Fatal("the upstream fixtures hold no notes, so the comparison proves nothing")
	}
}

// The original and the port run in one checker over the same fixture modules, staticcheck's and the house's two
// parity modules, the one older than Go 1.21 and the one newer, each with files tagged older and newer than its
// module, and the house's holding the types of every kind binary.Write is given and the shapes staticcheck's
// representation builds an argument or a call in more than one way, and their findings must be the same multiset of
// file, start, end and message. Each side's count is logged, since two empty sides would match and prove nothing, and
// the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original := []finding{}
	port := []finding{}
	for _, module := range append(slices.Clone(upstreamModules), "parity", "parity_current") {
		moduleOriginal, modulePort := findingsOf(test, module)
		test.Logf("%s: the original found %d, the port %d", module, len(moduleOriginal), len(modulePort))
		original = append(original, moduleOriginal...)
		port = append(port, modulePort...)
	}
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
// original makes and the port does not, unrecorded, fails, since the port is never worse than the original. The
// fixture holds the calls where staticcheck_call takes another path than callcheck and records no difference, since
// there is none, so the test holds that the two agree there, and fails should the original find nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
	expectedPort, expectedOriginal := recorded(test, "differences")
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
}

// In a package-level variable's initializer, the original asks the standard library's version of the package
// initializer's file, which there is none of, and panics when the value's type holds a bool, in a goroutine of the
// checker the test cannot recover, so the original does not run over the package_initializers fixture; the panic was
// seen in staticcheck's code.StdlibVersion, "no file found for node". The port runs alone there and must find exactly
// what the fixture records, the version read from the file of each call.
func TestPortJudgesWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	graph := analyze(test, []*analysis.Analyzer{Analyzer}, "package_initializers")
	port := []finding{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		if action.Analyzer != Analyzer {
			continue
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(test, action, diagnostic)
			port = append(port, finding{file: found.file, line: found.line, message: found.message})
		}
	}
	expectedPort, expectedOriginal := recorded(test, "package_initializers")
	test.Logf("the port found %d, the fixture records %d", len(port), len(expectedPort))
	if len(expectedPort) == 0 || len(expectedOriginal) > 0 {
		test.Fatal("the fixture must record what the port alone finds, and no renaming, since the original never runs")
	}
	compareAdjudicated(test, "the port", port, expectedPort)
}

// recorded is what the comments of the fixture module name record, its one file named for it: the findings the
// port alone makes, and the original's side of each renaming.
func recorded(test *testing.T, name string) (expectedPort []finding, expectedOriginal []finding) {
	test.Helper()
	expectedPort = []finding{}
	expectedOriginal = []finding{}
	relative := filepath.Join(name, name+".go")
	file, openError := os.Open(filepath.Join(analysistest.TestData(), relative))
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
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", relative, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: relative, line: line, message: match[1]})
			default:
				expectedOriginal = append(expectedOriginal, finding{file: relative, line: line, message: match[2]})
				expectedPort = append(expectedPort, finding{file: relative, line: line, message: match[3]})
			}
		}
	}
	if scanError := fileScanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return expectedPort, expectedOriginal
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

// finding is one finding as the differential compares it, its file named from the fixtures' folder, since every
// upstream module holds a file of the same name, and its range from start to end.
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
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// findingOf is diagnostic, reported by action, as a finding.
func findingOf(test *testing.T, action *checker.Action, diagnostic analysis.Diagnostic) finding {
	test.Helper()
	position := action.Package.Fset.Position(diagnostic.Pos)
	end := action.Package.Fset.Position(diagnostic.End)
	relative, relativeError := filepath.Rel(analysistest.TestData(), position.Filename)
	if relativeError != nil {
		test.Fatal(relativeError)
	}
	return finding{
		file:      relative,
		line:      position.Line,
		column:    position.Column,
		endLine:   end.Line,
		endColumn: end.Column,
		message:   diagnostic.Message,
	}
}

// analyze loads every package of the fixture module in testdata/<module> and runs analyzers over them in one
// checker. A root package that does not type-check fails the test, since the checker skips it and would compare
// nothing.
func analyze(test *testing.T, analyzers []*analysis.Analyzer, module string) *checker.Graph {
	test.Helper()
	directory := filepath.Join(analysistest.TestData(), module)
	loaded, loadError := load.Packages(directory, []string{"./..."}, moduleEnvironment, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", module, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %s found no package", module)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", module, analyzeError)
	}
	return graph
}

// findingsOf runs staticcheck's SA1003 and the port over the fixture module in testdata/<module> in one checker, and
// returns each one's findings, sorted.
func findingsOf(test *testing.T, module string) (original []finding, port []finding) {
	test.Helper()
	graph := analyze(test, []*analysis.Analyzer{sa1003.Analyzer, Analyzer}, module)
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			found := findingOf(test, action, diagnostic)
			switch action.Analyzer {
			case sa1003.Analyzer:
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
		dropped = append(dropped, finding{file: found.file, line: found.line, message: found.message})
	}
	return dropped
}
