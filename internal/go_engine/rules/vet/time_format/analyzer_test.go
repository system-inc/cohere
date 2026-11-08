package time_format

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/passes/timeformat"
	"golang.org/x/tools/txtar"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for timeformat, copied unchanged into testdata/src, a.go beside its golden file and the b it
// imports, and run as x/tools' test runs them, with suggested fixes and no flags: every `// want` the original
// expects, the port reports at the same line with the same message, analysistest fails on a finding no want expects as
// much as on a want nothing found, and the port's fixes, applied, turn a.go into a.go.golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixtures and those written for the port, parity holding every branch of the judgment and the shapes
// most likely to differ and dotted a dot import, the original and the port find the same findings, each the same in
// every field, each fix's edits compared as offsets and text, since a golden file is gofmt'd first and would miss an
// edit off by one byte.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, timeformat.Analyzer, Analyzer, analysistest.TestData(), "a", "parity", "dotted")
}

// Over the differences fixtures the two disagree, and only where each disagreement was adjudicated in the port's
// favour, as the fixtures record it on the finding's line, with the reason on the line above: a move is a finding
// both make, the original's over the wrong bytes, and a find a true finding only the port makes, each edit of its fix
// a `fixes:` record of the text it writes, so the find at a named constant, with none, offers no edit. The differences
// package is gofmt-clean and lives in testdata/src. The carriage returns of carriage.go are not, since gofmt drops a
// raw literal's, so it lives in carriage.txtar and is extracted into a directory of the test's own.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, timeformat.Analyzer, Analyzer, analysistest.TestData(), "differences")
	parity.RequireOnlyAdjudicated(test, timeformat.Analyzer, Analyzer, extractedCarriage(test), "carriage")
}

// The harness pairs a move by line and message alone, and holds a find's fixes only by the line each edit starts on
// and the text it writes, so over the same fixtures this holds what the records cannot: each fix applied. Every
// finding the port alone makes that has an end, a move's or a find's, offers one fix, one edit of its own span, on one
// line, to goodFormat, replacing source that spells badFormat alone and leaving the literal's value as it was with
// goodFormat in the mistake's place, so the span is the bytes that spell the mistake and no others. One with no end, a
// layout that is not one literal, sits at a name and offers no fix: its `finds:` record, with no `fixes:` record after
// it, counts edits, not fixes, so a fix with no edits there would pass the record, and only this catches it. And every
// finding the original alone makes offers a fix that fails the same check, since a move whose original fix were right
// would be no difference, and a `moves:` record holds only that the two differ.
func TestPortsFixesReplaceTheMistakeAlone(test *testing.T) {
	test.Parallel()
	checkFixes(test, analysistest.TestData(), "differences")
	checkFixes(test, extractedCarriage(test), "carriage")
}

// extractedCarriage writes carriage.txtar's files, carriage returns and all, into a directory of the test's own laid
// out as testdata is, and returns it.
func extractedCarriage(test *testing.T) string {
	test.Helper()
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
	return extracted
}

// checkFixes runs both over the package fixture names in testdata, a package of one file named for it, and checks
// each finding one side alone makes against the source, as TestPortsFixesReplaceTheMistakeAlone says.
func checkFixes(test *testing.T, testdata string, fixture string) {
	test.Helper()
	original, port := parity.Findings(test, timeformat.Analyzer, Analyzer, testdata, fixture)
	content, readError := os.ReadFile(filepath.Join(testdata, "src", fixture, fixture+".go"))
	if readError != nil {
		test.Fatal(readError)
	}
	lineStarts := []int{0}
	for offset, character := range content {
		if character == '\n' {
			lineStarts = append(lineStarts, offset+1)
		}
	}
	originalAlone := parity.Difference(original, port)
	for _, found := range originalAlone {
		if checkFix(content, lineStarts, found) == nil {
			test.Errorf("the original's fix at %s is right, so its line records no difference", found)
		}
	}
	portAlone := parity.Difference(port, original)
	if len(portAlone) == 0 {
		test.Fatalf("over %s, the port alone finds nothing, so no fix is checked", fixture)
	}
	withoutEnd := 0
	for _, found := range portAlone {
		if found.EndLine == 0 {
			withoutEnd++
			if len(found.Fixes) > 0 {
				test.Errorf("the port found %s, which has no end, and offers a fix", found)
			}
			if !startsName(content, offsetOf(lineStarts, found.Line, found.Column)) {
				test.Errorf("the port found %s, which has no end, and sits at no name", found)
			}
			continue
		}
		if found.EndLine != found.Line {
			test.Errorf("the port found %s, which spans more than its line", found)
		}
		if fixError := checkFix(content, lineStarts, found); fixError != nil {
			test.Errorf("the port's fix at %s is wrong: %v", found, fixError)
		}
	}
	test.Logf(
		"over %s, %d fixes of the original's are wrong, and %d of the port's are right, %d with no end and no fix",
		fixture, len(originalAlone), len(portAlone)-withoutEnd, withoutEnd,
	)
}

// offsetOf is the byte offset of line and column, both counted from 1 as a token.Position counts them, in a file
// whose lines open at lineStarts.
func offsetOf(lineStarts []int, line int, column int) int {
	return lineStarts[line-1] + column - 1
}

// startsName is whether an identifier opens at offset in content, parsed as a Go file.
func startsName(content []byte, offset int) bool {
	files := token.NewFileSet()
	parsed, parseError := parser.ParseFile(files, "fixture.go", content, parser.SkipObjectResolution)
	if parseError != nil {
		return false
	}
	for node := range ast.Preorder(parsed) {
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier &&
			files.Position(identifier.Pos()).Offset == offset {
			return true
		}
	}
	return false
}

// checkFix applies found's one fix to content, whose lines open at lineStarts, and reports what is wrong with it:
// anything but one edit of found's own span to goodFormat, a span whose source does not spell badFormat alone, or a
// literal whose text after the edit does not decode to its value before with its first badFormat replaced by
// goodFormat.
func checkFix(content []byte, lineStarts []int, found parity.Finding) error {
	offset := offsetOf(lineStarts, found.Line, found.Column)
	endOffset := offsetOf(lineStarts, found.EndLine, found.EndColumn)
	expectedFixes := []parity.Fix{{
		Message: "Replace " + badFormat + " with " + goodFormat,
		Edits:   []parity.Edit{{File: found.File, Start: offset, End: endOffset, NewText: goodFormat}},
	}}
	if !slices.EqualFunc(found.Fixes, expectedFixes, func(fix parity.Fix, expected parity.Fix) bool {
		return fix.Message == expected.Message && slices.Equal(fix.Edits, expected.Edits)
	}) {
		return fmt.Errorf("its fixes are %v, not one edit of its own span, %v", found.Fixes, expectedFixes)
	}
	before, beforeError := literalAt(content, offset)
	if beforeError != nil {
		return fmt.Errorf("before the fix: %w", beforeError)
	}
	// replaced is the source the edit replaces, which must decode, as the literal's own quotes decode it, to the
	// mistake and nothing more. A raw literal's carriage returns decode to nothing.
	replaced := string(content[offset:endOffset])
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
	fixed := slices.Concat(content[:offset], []byte(goodFormat), content[endOffset:])
	after, afterError := literalAt(fixed, offset)
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
