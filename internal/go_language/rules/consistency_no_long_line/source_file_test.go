package consistency_no_long_line

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// shortSource holds every shape the syntax walk gathers, an import, a //go: directive, comments, string literals, a
// struct field with a tag, parameters and results, a return, a key and a named string, all on lines within the limit.
const shortSource = `package short

import "strings"

//go:generate echo short

// Record is short.
type Record struct {
	Name string ` + "`json:\"name\"`" + `
}

const greeting = "hello"

func describe(record Record, count int) (string, error) {
	name := "short"
	_ = map[string]string{"key": "value"}
	return strings.Repeat(name+record.Name, count), nil
}
`

// TestFileWithoutALongLineSkipsTheWalk holds that a file with no line past the limit is never walked: its exemptions
// stay ungathered and reading its reported lines allocates only the file's own record, where the walk allocates the
// exemptions' maps and lists. A file with a line past the limit is still walked, so the skip is the only change.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestFileWithoutALongLineSkipsTheWalk(test *testing.T) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "short.go", shortSource, parser.ParseComments)
	if parseError != nil {
		test.Fatalf("parsing the short file: %v", parseError)
	}
	tokenFile := fileSet.File(file.FileStart)
	content := []byte(shortSource)
	source := newSourceFile(file, tokenFile, content)
	if len(source.longLines) != 0 || source.exemptLines != nil || source.unwrappableOpenings != nil ||
		source.unwrappableFields != nil || source.stringLiterals != nil || source.comments != nil {
		test.Errorf("a file with no long line was walked: %d long lines, %d string literals, %d comments",
			len(source.longLines), len(source.stringLiterals), len(source.comments))
	}
	allocations := testing.AllocsPerRun(10, func() { ReportedLines(file, tokenFile, content) })
	test.Logf("allocations reading a file with no long line: %.0f", allocations)
	if allocations > 1 {
		test.Errorf("reading a file with no long line allocated %.0f times, more than its own record: it was walked",
			allocations)
	}

	longSource := shortSource + "\nvar long = describe /* " + strings.Repeat("and so on ", 12) + "*/\n"
	longFile, longParseError := parser.ParseFile(fileSet, "long.go", longSource, parser.ParseComments)
	if longParseError != nil {
		test.Fatalf("parsing the long file: %v", longParseError)
	}
	longToken := fileSet.File(longFile.FileStart)
	walked := newSourceFile(longFile, longToken, []byte(longSource))
	if len(walked.longLines) != 1 || len(walked.stringLiterals) == 0 || len(walked.comments) == 0 {
		test.Fatalf("a file with a long line was not walked: %d long lines, %d string literals, %d comments",
			len(walked.longLines), len(walked.stringLiterals), len(walked.comments))
	}
	if reported := ReportedLines(longFile, longToken, []byte(longSource)); len(reported) != 1 ||
		reported[0] != longToken.LineCount() {
		test.Errorf("the long file reports lines %v, where its last line, %d, is the one long", reported,
			longToken.LineCount())
	}
}

// TestLongLinesAgreeWithTheMeasureAtTheBound holds that the bound gatherLongLines skips a line on, its bytes with each
// tab's extra columns added, never skips a line the rune by rune measure finds past the limit. Each line sits at the
// limit or one past it, where the bound and the measure meet, with tabs, multi-byte letters, a byte that is no rune and
// a carriage return, and each is checked against both columnsOf and the width a reader counts by hand.
func TestLongLinesAgreeWithTheMeasureAtTheBound(test *testing.T) {
	test.Parallel()
	cases := []struct {
		text   string
		length int
	}{
		{text: "", length: 0},
		{text: strings.Repeat("a", 120), length: 120},
		{text: strings.Repeat("a", 121), length: 121},
		{text: strings.Repeat("\t", 30), length: 120},
		{text: strings.Repeat("\t", 30) + "a", length: 121},
		{text: strings.Repeat("\t", 29) + "abcd", length: 120},
		{text: strings.Repeat("\t", 29) + "abcde", length: 121},
		{text: "\t" + strings.Repeat("a", 116), length: 120},
		{text: "\t" + strings.Repeat("a", 117), length: 121},
		// Past the limit in bytes and at it in runes, so the bound cannot skip it and the measure must clear it.
		{text: strings.Repeat("é", 120), length: 120},
		{text: strings.Repeat("é", 121), length: 121},
		{text: "\t" + strings.Repeat("語", 116), length: 120},
		{text: "\t" + strings.Repeat("語", 117), length: 121},
		// A byte that is no rune decodes as one column.
		{text: strings.Repeat("\xff", 120), length: 120},
		{text: strings.Repeat("\xff", 121), length: 121},
		// The carriage return of a line ending is no part of the line.
		{text: strings.Repeat("a", 120) + "\r", length: 120},
	}
	var content []byte
	for _, testCase := range cases {
		content = append(append(content, testCase.text...), '\n')
	}
	tokenFile := token.NewFileSet().AddFile("bound.go", -1, len(content))
	tokenFile.SetLinesForContent(content)
	source := &sourceFile{tokenFile: tokenFile, content: content, lines: tokenFile.Lines()}
	gathered := map[int]longLine{}
	for _, long := range source.gatherLongLines() {
		gathered[long.line] = long
	}
	for index, testCase := range cases {
		line := index + 1
		measured := columnsOf(source.content[source.lineStart(line):source.lineEnd(line)])
		if measured != testCase.length {
			test.Errorf("line %d, %q, measures %d columns, where a reader counts %d", line, testCase.text, measured,
				testCase.length)
		}
		long, found := gathered[line]
		if found != (testCase.length > Limit) {
			test.Errorf("line %d, %q, of %d columns, gathered as long: %t", line, testCase.text, testCase.length, found)
			continue
		}
		if found && long.length != testCase.length {
			test.Errorf("line %d, %q, gathered at %d columns, where it is %d", line, testCase.text, long.length,
				testCase.length)
		}
	}
}
