package time_format

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// x/tools' own fixtures for timeformat, copied unchanged into testdata/src, a.go beside its golden file and the b it
// imports, and run as x/tools' test runs them, with suggested fixes and no flags: every `// want` the original
// expects, the port reports at the same line with the same message, analysistest fails on a finding no want expects as
// much as on a want nothing found, and the port's fixes, applied, turn a.go into a.go.golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a")
}

// The original and the port run in one checker over the same fixture packages, x/tools' and those written for the
// port, parity holding every branch of the judgment and the shapes most likely to differ and dotted a dot import, and
// their findings must be the same multiset of file, start, end, message and fixes, each fix's edits compared as
// offsets and text, since a golden file is gofmt'd first and would miss an edit off by one byte. Each side's count is
// logged, since two empty sides would match and prove nothing, and the test fails if the original finds nothing.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := findingsOf(test, analysistest.TestData(), "a", "parity", "dotted")
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

// recordPattern is one comment in a differences fixture, the text after its `// port ` opening: its kind, the message,
// `@` and the port's span on that line by column, its start and its end, or its start alone for a finding with no
// end. `moves:` is a finding both make on that line with that message, the port's over the pinned span and the
// original's elsewhere. `finds:` is a true finding only the port makes.
var recordPattern = regexp.MustCompile(`^(moves|finds): (.+?) @ (\d+)(?:-(\d+))?\s*$`)

// record is one comment of a differences fixture, read: its kind, and the port's finding it pins, by file, line,
// column, end column and message.
type record struct {
	kind  string
	found finding
}

// Over the differences fixtures the two disagree, and only where each disagreement was adjudicated in the port's
// favour, as the fixtures record it on the finding's line, with the reason on the line above. A move is a finding both
// make, the original's over the wrong bytes: the port's span is the one pinned, its fix, applied, replaces source that
// spells 2006-02-01 alone and leaves the literal's value as it was with 2006-01-02 in the mistake's place, and the
// original's fix does not. A finding the port alone makes is pinned the same way, its fix checked the same way, or it
// has no fix where it has no end. Every finding either side alone makes must be recorded, and every record matched by
// exactly one. The differences package is gofmt-clean and lives in testdata/src. The carriage returns of carriage.go
// are not, since gofmt drops a raw literal's, so it lives in carriage.txtar and is extracted into a directory of the
// test's own.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	compareDifferences(test, analysistest.TestData(), "differences")
	archive, archiveError := txtar.ParseFile(filepath.Join(analysistest.TestData(), "carriage.txtar"))
	if archiveError != nil {
		test.Fatal(archiveError)
	}
	extracted := test.TempDir()
	for _, file := range archive.Files {
		path := filepath.Join(extracted, filepath.FromSlash(file.Name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, file.Data, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	compareDifferences(test, extracted, "carriage")
}

// compareDifferences runs both over the package at pattern in the tree at directory, a package of one file named for
// it, and fails the test unless every finding either side alone makes is one the file records, as it records it.
func compareDifferences(test *testing.T, directory string, pattern string) {
	test.Helper()
	original, port := findingsOf(test, directory, pattern)
	test.Logf("over %s, the original found %d, the port %d", pattern, len(original), len(port))
	path := filepath.Join(directory, "src", pattern, pattern+".go")
	content, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	records := recordsOf(test, pattern+".go", content)
	originalAlone := unclaimedFindings(difference(original, port))
	portAlone := unclaimedFindings(difference(port, original))
	moves := 0
	finds := 0
	for _, recorded := range records {
		portFound := portAlone.claim(test, "the port", recorded.found)
		if recorded.kind == "moves" {
			moves++
			originalFound := originalAlone.claim(test, "the original", recorded.found)
			if originalFound != nil && checkFix(content, *originalFound) == nil {
				test.Errorf("the original's fix at %s is right, so %s:%d records no difference",
					described(*originalFound), recorded.found.file, recorded.found.line)
			}
		} else {
			finds++
		}
		if portFound == nil {
			continue
		}
		if portFound.column != recorded.found.column || portFound.endColumn != recorded.found.endColumn ||
			portFound.endLine != 0 && portFound.endLine != portFound.line {
			test.Errorf("the port found %s, and %s:%d pins its span to columns %d to %d of that line",
				described(*portFound), recorded.found.file, recorded.found.line, recorded.found.column,
				recorded.found.endColumn)
		}
		if portFound.endColumn == 0 {
			if portFound.fixes != "[]" {
				test.Errorf("the port found %s, which has no end, and offers a fix", described(*portFound))
			}
			continue
		}
		if fixError := checkFix(content, *portFound); fixError != nil {
			test.Errorf("the port's fix at %s is wrong: %v", described(*portFound), fixError)
		}
	}
	test.Logf("over %s, %d moves and %d findings only the port makes are recorded", pattern, moves, finds)
	for _, unrecorded := range originalAlone {
		test.Errorf("the original alone found %s, which the fixture does not record", described(unrecorded))
	}
	for _, unrecorded := range portAlone {
		test.Errorf("the port alone found %s, which the fixture does not record", described(unrecorded))
	}
}

// recordsOf reads every record of the differences fixture named file whose text is content, and fails the test on a
// comment it cannot read, on a record with no reason on the line above, and on a fixture that records nothing.
func recordsOf(test *testing.T, file string, content []byte) []record {
	test.Helper()
	lines := strings.Split(string(content), "\n")
	records := []record{}
	for index, text := range lines {
		comments := strings.Split(text, "// port ")[1:]
		if len(comments) > 1 {
			test.Fatalf("%s:%d holds %d records, and a line holds one", file, index+1, len(comments))
		}
		for _, comment := range comments {
			match := recordPattern.FindStringSubmatch(comment)
			if match == nil {
				test.Fatalf("%s:%d holds a comment the test cannot read: %q", file, index+1, comment)
			}
			// The line above is the reason, a comment of its own that is no record.
			if index == 0 || !strings.HasPrefix(strings.TrimSpace(lines[index-1]), "//") ||
				strings.Contains(lines[index-1], "// port ") {
				test.Fatalf("%s:%d records a difference with no reason on the line above", file, index+1)
			}
			column, columnError := strconv.Atoi(match[3])
			if columnError != nil {
				test.Fatalf("%s:%d pins a column the test cannot read: %v", file, index+1, columnError)
			}
			// A record with no end column pins a finding with no end, whose end column reads as zero.
			endColumn := 0
			if match[4] != "" {
				parsed, endError := strconv.Atoi(match[4])
				if endError != nil {
					test.Fatalf("%s:%d pins an end column the test cannot read: %v", file, index+1, endError)
				}
				endColumn = parsed
			}
			records = append(records, record{kind: match[1], found: finding{
				file: file, line: index + 1, column: column, endColumn: endColumn, message: match[2],
			}})
		}
	}
	if len(records) == 0 {
		test.Fatalf("%s records nothing, so the comparison proves nothing", file)
	}
	return records
}

// checkFix applies found's one fix to content and reports what is wrong with it: anything but one edit of found's
// own span to goodFormat, a span whose source does not spell badFormat alone, or a literal whose text after the edit
// does not decode to its value before with its first badFormat replaced by goodFormat.
func checkFix(content []byte, found finding) error {
	expectedFixes := fmt.Sprintf("[{%q %s[%d:%d]=%q}]", "Replace "+badFormat+" with "+goodFormat, found.file,
		found.offset, found.endOffset, goodFormat)
	if found.fixes != expectedFixes {
		return fmt.Errorf("its fixes are %s, not one edit of its own span, %s", found.fixes, expectedFixes)
	}
	before, beforeError := literalAt(content, found.offset)
	if beforeError != nil {
		return fmt.Errorf("before the fix: %w", beforeError)
	}
	// replaced is the source the edit replaces, which must decode, as the literal's own quotes decode it, to the
	// mistake and nothing more. A raw literal's carriage returns decode to nothing.
	replaced := string(content[found.offset:found.endOffset])
	decoded := strings.ReplaceAll(replaced, "\r", "")
	if !strings.HasPrefix(before.Value, "`") {
		unquoted, unquoteError := strconv.Unquote(`"` + replaced + `"`)
		if unquoteError != nil {
			return fmt.Errorf("it replaces %q, which is not whole characters of the literal: %w", replaced,
				unquoteError)
		}
		decoded = unquoted
	}
	if decoded != badFormat {
		return fmt.Errorf("it replaces %q, which spells %q, not %s alone", replaced, decoded, badFormat)
	}
	fixed := slices.Concat(content[:found.offset], []byte(goodFormat), content[found.endOffset:])
	after, afterError := literalAt(fixed, found.offset)
	if afterError != nil {
		return fmt.Errorf("after the fix: %w", afterError)
	}
	beforeValue, beforeValueError := strconv.Unquote(before.Value)
	if beforeValueError != nil {
		return fmt.Errorf("before the fix, %s: %w", before.Value, beforeValueError)
	}
	afterValue, afterValueError := strconv.Unquote(after.Value)
	if afterValueError != nil {
		return fmt.Errorf("it leaves %s, which does not decode: %w", after.Value, afterValueError)
	}
	if expected := strings.Replace(beforeValue, badFormat, goodFormat, 1); afterValue != expected {
		return fmt.Errorf("it turns %s into %s, whose value is %q, not %q", before.Value, after.Value, afterValue,
			expected)
	}
	return nil
}

// literalAt parses content as a Go file and returns the last string literal that opens at or before offset, the one a
// finding inside a literal lies in.
func literalAt(content []byte, offset int) (*ast.BasicLit, error) {
	files := token.NewFileSet()
	parsed, parseError := parser.ParseFile(files, "fixture.go", content, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	var enclosing *ast.BasicLit
	for node := range ast.Preorder(parsed) {
		literal, isLiteral := node.(*ast.BasicLit)
		if isLiteral && literal.Kind == token.STRING && files.Position(literal.Pos()).Offset <= offset {
			enclosing = literal
		}
	}
	if enclosing == nil {
		return nil, fmt.Errorf("no string literal opens at or before offset %d", offset)
	}
	return enclosing, nil
}

// finding is one finding as the differential compares it. offset and endOffset are its span in bytes from the file's
// start, and fixes is every suggested fix written out, its message and each edit's offsets and text, so a finding stays
// one comparable value.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	offset    int
	endOffset int
	message   string
	fixes     string
}

// described is found as a finding line prints it, its end and fixes after.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message, found.fixes)
}

// findingsOf loads the fixture packages patterns name in the tree at directory, runs x/tools' timeformat and the port
// over them in one checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, directory string, patterns ...string) (original []finding, port []finding) {
	test.Helper()
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
		[]*analysis.Analyzer{timeformat.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			// A finding with no end, as one reported with Reportf, has an end position that is not valid, line and
			// column zero, which compares as well as any other.
			end := action.Package.Fset.Position(diagnostic.End)
			found := finding{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				offset:    position.Offset,
				endOffset: end.Offset,
				message:   diagnostic.Message,
				fixes:     fixesOf(action.Package.Fset, diagnostic),
			}
			switch action.Analyzer {
			case timeformat.Analyzer:
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

// fixesOf is every suggested fix of diagnostic written out, in order: its message, then each edit as the file, the
// byte offsets it replaces and the text it puts there.
func fixesOf(files *token.FileSet, diagnostic analysis.Diagnostic) string {
	written := []string{}
	for _, fix := range diagnostic.SuggestedFixes {
		edits := []string{}
		for _, edit := range fix.TextEdits {
			start := files.Position(edit.Pos)
			end := files.Position(edit.End)
			edits = append(edits, fmt.Sprintf("%s[%d:%d]=%q", filepath.Base(start.Filename), start.Offset, end.Offset,
				edit.NewText))
		}
		written = append(written, fmt.Sprintf("{%q %s}", fix.Message, strings.Join(edits, " ")))
	}
	return "[" + strings.Join(written, " ") + "]"
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

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
