package invalid_template

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
	"honnef.co/go/tools/staticcheck/sa1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for SA1001, copied unchanged from
// staticcheck/sa1001/testdata/go1.0/CheckTemplate into testdata/src/CheckTemplate. The go1.0 folder sets the language
// version of staticcheck's run, which SA1001 never reads, so the fixture loads from a tree laid out by import path as
// the house's fixtures do.
const upstream = "CheckTemplate"

// Every `//@ diag(...)` note the upstream fixture holds, read by staticcheck_wants, the port reports on the same line
// with a message the note matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wants, readError := staticcheck_wants.Read(filepath.Join(analysistest.TestData(), "src", upstream))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	graph := analyze(test, []*analysis.Analyzer{Analyzer}, upstream)
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
	test.Logf("the fixture wants %d, the port reported %d", len(wants), len(findings))
	unexpected, untaken := staticcheck_wants.Compare(wants, findings)
	for _, found := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", filepath.Base(found.File),
			found.Line, found.Message)
	}
	for _, want := range untaken {
		test.Errorf("the port reported nothing matching %s", want)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// way the query and its filters accept or refuse a call: each package's Parse on a template its New makes in the
// call, and on one made by Must, Delims, Funcs, a variable or a parenthesized New, a method value and a method
// expression, a function named Parse, and the template as a constant written in each way, inside parentheses and
// joined, as a value that is not constant, and each kind of error the template packages give, the syntax errors the
// original reports and the others it leaves, in a generated file too, and their findings must be the same multiset of
// file, start, end, message and fixes, of which neither makes any. Each side's count is logged, since two empty
// sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, upstream, "parity")
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
// fixture holds the shapes where the range could part, a template spread over lines, beside comments, inside
// parentheses and joined from constants, and records no difference, since there is none, so the test holds that the
// two agree there, and fails should the original find nothing in it. The one shape where they part, a parenthesized
// method, panics the original, so it has a fixture and a test of its own.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
	expectedPort, expectedOriginal := adjudicated(test, "differences")
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", withoutColumns(difference(port, original)), expectedPort)
	compareAdjudicated(test, "the original", withoutColumns(difference(original, port)), expectedOriginal)
}

// The original asserts that a Parse call's function is a selector once its query, which looks through parentheses,
// has matched, and panics on `(tt.New("").Parse)(text)`, so it reports nothing for that package, every other call in
// it included. The panics fixture holds that call and a plain one beside it: the original, run behind a recover, must
// still fail there, and the port must make exactly the findings the fixture records with `// port finds:`, both the
// parenthesized call's and the plain call's, which the original loses with it.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	recovering := &analysis.Analyzer{
		Name:     sa1001.Analyzer.Name,
		Doc:      sa1001.Analyzer.Doc,
		Requires: sa1001.Analyzer.Requires,
		Run: func(pass *analysis.Pass) (result any, runError error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					runError = fmt.Errorf("the original panicked: %v", recovered)
				}
			}()
			return sa1001.Analyzer.Run(pass)
		},
	}
	graph := analyze(test, []*analysis.Analyzer{recovering, Analyzer}, "panics")
	port := []finding{}
	panicked := false
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case recovering:
			panicked = action.Err != nil
			test.Logf("the original: %v", action.Err)
		case Analyzer:
			if action.Err != nil {
				test.Fatalf("running %s: %v", action, action.Err)
			}
			for _, diagnostic := range action.Diagnostics {
				position := action.Package.Fset.Position(diagnostic.Pos)
				port = append(port, finding{
					file: filepath.Base(position.Filename), line: position.Line, message: diagnostic.Message,
				})
			}
		}
	}
	if !panicked {
		test.Error("the original no longer panics on the panics fixture, so the port's difference there is unproven")
	}
	expectedPort, _ := adjudicated(test, "panics")
	test.Logf("the fixture records %d findings, the port made %d", len(expectedPort), len(port))
	if len(expectedPort) == 0 {
		test.Fatal("the panics fixture records nothing, so the comparison proves nothing")
	}
	compareAdjudicated(test, "the port", port, expectedPort)
}

// adjudicated is what the fixture package named pattern records in its file of the same name: the findings the port
// alone makes, and the original's side of every renaming. A comment the test cannot read fails it.
func adjudicated(test *testing.T, pattern string) (expectedPort []finding, expectedOriginal []finding) {
	test.Helper()
	name := pattern + ".go"
	path := filepath.Join(analysistest.TestData(), "src", pattern, name)
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	expectedPort = []finding{}
	expectedOriginal = []finding{}
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", name, line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: name, line: line, message: match[1]})
			default:
				expectedOriginal = append(expectedOriginal, finding{file: name, line: line, message: match[2]})
				expectedPort = append(expectedPort, finding{file: name, line: line, message: match[3]})
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

// finding is one finding as the differential compares it, its range from start to end and its fixes, described.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
	fixes     string
}

// described is found as a finding line prints it, with its end and its fixes.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s%s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message, found.fixes)
}

// suggestedFix is one fix a finding suggests, its message and its edits, in the order the finding holds them.
type suggestedFix struct {
	message string
	edits   []fixEdit
}

// fixEdit is one edit of a fix, by its file and byte offsets in it, so it applies to the file's bytes.
type fixEdit struct {
	file    string
	start   int
	end     int
	newText string
}

// fullDiagnostic is one finding with everything the tests compare or apply: its finding and its fixes.
type fullDiagnostic struct {
	finding finding
	fixes   []suggestedFix
}

// analyze loads the fixture packages patterns name and runs analyzers over them in one checker.
func analyze(test *testing.T, analyzers []*analysis.Analyzer, patterns ...string) *checker.Graph {
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
		// A fixture that does not type-check is one the checker skips, and would compare nothing.
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	return graph
}

// diagnosticsOf runs analyzers over the fixture packages patterns name in one checker, and returns each one's
// findings with their fixes, by the analyzer that made them.
func diagnosticsOf(
	test *testing.T,
	analyzers []*analysis.Analyzer,
	patterns ...string,
) map[*analysis.Analyzer][]fullDiagnostic {
	test.Helper()
	graph := analyze(test, analyzers, patterns...)
	diagnostics := map[*analysis.Analyzer][]fullDiagnostic{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			found := fullDiagnostic{finding: finding{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
			}}
			fixes := &strings.Builder{}
			for _, suggested := range diagnostic.SuggestedFixes {
				each := suggestedFix{message: suggested.Message}
				fmt.Fprintf(fixes, " fix %q", suggested.Message)
				for _, textEdit := range suggested.TextEdits {
					start := action.Package.Fset.Position(textEdit.Pos)
					edited := fixEdit{
						file:    filepath.Base(start.Filename),
						start:   start.Offset,
						end:     action.Package.Fset.Position(textEdit.End).Offset,
						newText: string(textEdit.NewText),
					}
					each.edits = append(each.edits, edited)
					fmt.Fprintf(fixes, " %s[%d,%d) %q", edited.file, edited.start, edited.end, edited.newText)
				}
				found.fixes = append(found.fixes, each)
			}
			found.finding.fixes = fixes.String()
			diagnostics[action.Analyzer] = append(diagnostics[action.Analyzer], found)
		}
	}
	return diagnostics
}

// findingsOf runs staticcheck's SA1001 and the port over the fixture packages patterns name in one checker, and
// returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	diagnostics := diagnosticsOf(test, []*analysis.Analyzer{sa1001.Analyzer, Analyzer}, patterns...)
	for _, found := range diagnostics[sa1001.Analyzer] {
		original = append(original, found.finding)
	}
	for _, found := range diagnostics[Analyzer] {
		port = append(port, found.finding)
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

// withoutColumns is findings with their columns, ends and fixes dropped, for comparing with the differences
// fixture's comments, which name a line and a message.
func withoutColumns(findings []finding) []finding {
	dropped := []finding{}
	for _, found := range findings {
		dropped = append(dropped, finding{file: found.file, line: found.line, message: found.message})
	}
	return dropped
}
