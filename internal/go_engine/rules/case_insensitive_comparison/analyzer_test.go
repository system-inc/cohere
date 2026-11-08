package case_insensitive_comparison

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/staticcheck/sa6005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for SA6005, copied unchanged from
// staticcheck/sa6005/testdata/go1.0/CheckToLowerToUpperComparison into testdata/src/CheckToLowerToUpperComparison with
// its golden file. The go1.0 folder sets the language version of staticcheck's run, which SA6005 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckToLowerToUpperComparison"

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

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil
// (analysis/lint/testutil/check.go) requires of the original's. The golden file is a txtar archive: with sections,
// each is named for a fix's message and holds the fixture with every edit of the fixes of that message applied, and
// a comment beside them is refused; with none, its comment is the fixture with every edit applied. The fixed source
// is gofmt'd before it is compared, and a section's trailing newlines are cut to one.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	path := filepath.Join(analysistest.TestData(), "src", upstream, upstream+".go")
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	golden, goldenError := os.ReadFile(path + ".golden")
	if goldenError != nil {
		test.Fatal(goldenError)
	}
	archive := txtar.Parse(golden)
	editsByFix := map[string][]fixEdit{}
	for _, found := range diagnosticsOf(test, []*analysis.Analyzer{Analyzer}, upstream)[Analyzer] {
		for _, suggested := range found.fixes {
			editsByFix[suggested.message] = append(editsByFix[suggested.message], suggested.edits...)
		}
	}
	test.Logf("the port suggested %d kinds of fix, the golden file holds %d sections", len(editsByFix),
		len(archive.Files))
	if len(editsByFix) == 0 {
		test.Fatal("the port suggested no fix in the upstream fixture, so the comparison proves nothing")
	}
	if len(archive.Files) == 0 {
		all := []fixEdit{}
		for _, edits := range editsByFix {
			all = append(all, edits...)
		}
		compareFixed(test, "the golden file", applied(test, source, all), archive.Comment)
		return
	}
	if len(archive.Comment) != 0 {
		test.Fatal("the golden file holds sections and a comment beside them, which staticcheck's testutil refuses")
	}
	for message, edits := range editsByFix {
		index := slices.IndexFunc(archive.Files, func(section txtar.File) bool { return section.Name == message })
		if index < 0 {
			test.Errorf("the golden file holds no section for the fix %q", message)
			continue
		}
		expected := append(bytes.TrimRight(archive.Files[index].Data, "\n"), '\n')
		compareFixed(test, "the section "+message, applied(test, source, edits), expected)
	}
	for _, section := range archive.Files {
		if _, isSuggested := editsByFix[section.Name]; !isSuggested {
			test.Errorf("the golden file holds a section for the fix %q, which the port never suggested", section.Name)
		}
	}
}

// applied is source with edits applied, gofmt'd as staticcheck's testutil formats it.
func applied(test *testing.T, source []byte, edits []fixEdit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
	slices.SortFunc(ordered, func(first fixEdit, second fixEdit) int {
		return cmp.Compare(second.start, first.start)
	})
	fixed := slices.Clone(source)
	for index, each := range ordered {
		if index > 0 && each.end > ordered[index-1].start {
			test.Fatalf("the port's edits overlap at offset %d", each.start)
		}
		fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
	}
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
	}
	return formatted
}

// compareFixed fails the test unless the fixed source is what the golden file's part named where holds.
func compareFixed(test *testing.T, where string, fixed []byte, expected []byte) {
	test.Helper()
	if !bytes.Equal(fixed, expected) {
		test.Errorf("the port's fixes make\n%s\nwhere %s holds\n%s", fixed, where, expected)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every way
// the pattern accepts or refuses a comparison: the operators, the functions on each side and whether they agree,
// parentheses around the calls, their functions and their arguments, functions held in variables or of the bytes
// package, and the strings package imported under another name and into a file's own scope, and their findings must
// be the same multiset of file, start, end, message and fixes, each fix's message and each edit's file, offsets and
// text. Each side's count is logged, since two empty sides would match and prove nothing, and the test fails if the
// original finds nothing at all.
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
// fixture holds the comparisons where the port's reading or the fix it prints could part from the pattern's,
// parenthesized arguments, a parenthesized function on one side, a comparison over two lines and calls as
// arguments, and records no difference, since there is none, so the test holds that the two agree there, fixes
// included, and fails should the original find nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
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
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[1]})
			default:
				renamed := finding{file: "differences.go", line: line, message: match[2]}
				expectedOriginal = append(expectedOriginal, renamed)
				expectedPort = append(expectedPort, finding{file: "differences.go", line: line, message: match[3]})
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

// findingsOf runs staticcheck's SA6005 and the port over the fixture packages patterns name in one checker, and
// returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	diagnostics := diagnosticsOf(test, []*analysis.Analyzer{sa6005.Analyzer, Analyzer}, patterns...)
	for _, found := range diagnostics[sa6005.Analyzer] {
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
