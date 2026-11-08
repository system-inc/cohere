package signal_channel_analyzer

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
	"golang.org/x/tools/go/analysis/passes/sigchanyzer"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixture for sigchanyzer, copied unchanged into testdata/src with its golden file and run as x/tools'
// test runs it, with suggested fixes and no flags: every `// want` the original expects, the port reports at the
// same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, and every fix the port offers, applied, turns the file into its golden one.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and of the fix and the shapes most likely to differ, and their findings must be
// the same multiset of file, position, end, message and fix, edit for edit. Each side's count is logged, since two
// empty sides would match and prove nothing, and the test fails if the original finds nothing at all.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "a", "parity")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(original, port, described) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(port, original, described) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, on the line the finding starts, or `fixes:` and the text one of those
// findings' fixes puts in place of what starts on the comment's line.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|fixes: (.+?))\s*$`)

// Over the differences fixture the two disagree, and only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, every edit of its fixes is recorded on the line
// it starts, and every record is matched. The original makes no finding the port does not, since the port is never
// worse than the original, and the sides' common findings show the fixture is read the same by both.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	expectedFindings := []finding{}
	expectedEdits := []edit{}
	paths, globError := filepath.Glob(filepath.Join(analysistest.TestData(), "src", "differences", "*.go"))
	if globError != nil {
		test.Fatal(globError)
	}
	for _, path := range paths {
		for line, comment := range adjudicatedComments(test, path) {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", filepath.Base(path), line, comment)
			case match[1] != "":
				expectedFindings = append(expectedFindings, finding{
					file: filepath.Base(path), line: line, message: match[1],
				})
			default:
				expectedEdits = append(expectedEdits, edit{file: filepath.Base(path), line: line, text: match[2]})
			}
		}
	}
	if len(expectedFindings) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	portAlone := difference(port, original, described)
	for _, unrecorded := range difference(withoutDetail(portAlone), expectedFindings, described) {
		test.Errorf("the port alone found %s, which the fixture does not record", described(unrecorded))
	}
	for _, unmatched := range difference(expectedFindings, withoutDetail(portAlone), described) {
		test.Errorf("the fixture records that the port alone finds %s, and it does not", described(unmatched))
	}
	editsAlone := []edit{}
	for _, found := range portAlone {
		editsAlone = append(editsAlone, found.edits...)
	}
	for _, unrecorded := range difference(editsAlone, expectedEdits, editDescribed) {
		test.Errorf("the port alone fixes %s, which the fixture does not record", editDescribed(unrecorded))
	}
	for _, unmatched := range difference(expectedEdits, editsAlone, editDescribed) {
		test.Errorf("the fixture records that the port alone fixes %s, and it does not", editDescribed(unmatched))
	}
	for _, originalAlone := range difference(original, port, described) {
		test.Errorf("the original alone found %s, and the port is never weaker", described(originalAlone))
	}
}

// adjudicatedComments is every `// port ` comment in the file at path, the text after its opening, keyed by its line.
// A line holds one at most, since a comment holds the rest of its line.
func adjudicatedComments(test *testing.T, path string) map[int]string {
	test.Helper()
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	comments := map[int]string{}
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		if _, comment, holdsOne := strings.Cut(scanner.Text(), "// port "); holdsOne {
			comments[line] = comment
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	return comments
}

// finding is one finding as the differential compares it.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
	// fixes is the finding's suggested fixes, each its message and its edits as offsets into the file and the text
	// put there, so two findings with the same text but different edits differ.
	fixes string
	// edits is every edit of the fixes by the file and line it starts at, for the differences fixture's records.
	edits []edit
}

// described is found as a finding line prints it, with its end, followed by its fixes.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message, found.fixes)
}

// withoutDetail is findings with only their file, line and message kept, for comparing with the differences fixture's
// comments, which name a line.
func withoutDetail(findings []finding) []finding {
	kept := []finding{}
	for _, found := range findings {
		kept = append(kept, finding{file: found.file, line: found.line, message: found.message})
	}
	return kept
}

// edit is one edit of a suggested fix as the differences fixture records it: the file and line it starts at and the
// text it puts there.
type edit struct {
	file string
	line int
	text string
}

// editDescribed is recorded as a failure names it.
func editDescribed(recorded edit) string {
	return fmt.Sprintf("%s:%d to %q", recorded.file, recorded.line, recorded.text)
}

// findingsOf loads the fixture packages patterns name, runs x/tools' sigchanyzer and the port over them in one
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
		[]*analysis.Analyzer{sigchanyzer.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			fixes, edits := fixesOf(action, diagnostic)
			found := finding{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   diagnostic.Message,
				fixes:     fixes,
				edits:     edits,
			}
			switch action.Analyzer {
			case sigchanyzer.Analyzer:
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

// fixesOf is diagnostic's suggested fixes, each its message and every edit as `file[start,end)"text"` with offsets
// into the edit's file, in the order the fix lists them, and the same edits by the file and line each starts at.
func fixesOf(action *checker.Action, diagnostic analysis.Diagnostic) (string, []edit) {
	fixes := []string{}
	edits := []edit{}
	for _, fix := range diagnostic.SuggestedFixes {
		editTexts := []string{}
		for _, textEdit := range fix.TextEdits {
			file := action.Package.Fset.File(textEdit.Pos)
			name := filepath.Base(file.Name())
			editTexts = append(editTexts, fmt.Sprintf(
				"%s[%d,%d)%q", name, file.Offset(textEdit.Pos), file.Offset(textEdit.End), textEdit.NewText,
			))
			edits = append(edits, edit{file: name, line: file.Line(textEdit.Pos), text: string(textEdit.NewText)})
		}
		fixes = append(fixes, fmt.Sprintf("{%s: %s}", fix.Message, strings.Join(editTexts, " ")))
	}
	return strings.Join(fixes, " "), edits
}

// difference is what of from is not in without, counting repeats and comparing by key, so a finding reported twice
// by one side and once by the other is a difference.
func difference[Item any](from []Item, without []Item, key func(Item) string) []Item {
	remaining := map[string]int{}
	for _, item := range without {
		remaining[key(item)]++
	}
	missing := []Item{}
	for _, item := range from {
		if remaining[key(item)] > 0 {
			remaining[key(item)]--
			continue
		}
		missing = append(missing, item)
	}
	return missing
}

// errorsOnly hands analysistest.RunWithSuggestedFixes a reporter that isn't a testing.TB. Given a testing.TB, Run
// first proves the go command works by building a throwaway program, and a runner of prebuilt test binaries refuses a
// test that builds. Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
