package core

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const maxLinesFile = "/repository/source/Long.ts"

// maxLinesSource is count statements, one per line, joined by lineBreak and ended by it when
// trailing is set.
func maxLinesSource(count int, lineBreak string, trailing bool) string {
	lines := make([]string, count)
	for index := range lines {
		lines[index] = "export const value" + strconv.Itoa(index) + " = 1;"
	}
	source := strings.Join(lines, lineBreak)
	if trailing {
		source += lineBreak
	}
	return source
}

// The count is the one an editor shows and cohere's Swift rule max-file-lines uses: a final line
// break ends the last line rather than starting another, and every line break upstream knows is one.
func TestMaxLinesCountsLinesAsAnEditorShowsThem(test *testing.T) {
	test.Parallel()

	atLimit := MaxLinesOptions{Maximum: 2000}
	cases := []struct {
		name     string
		source   string
		findings int
	}{
		{"2,000 lines ending in a newline are 2,000", maxLinesSource(2000, "\n", true), 0},
		{"2,000 lines with no final newline", maxLinesSource(2000, "\n", false), 0},
		{"2,001 lines report", maxLinesSource(2001, "\n", true), 1},
		{"\\r\\n is one line ending", maxLinesSource(2000, "\r\n", true), 0},
		{"\\r\\n, one line over", maxLinesSource(2001, "\r\n", true), 1},
		{"a lone \\r breaks a line", maxLinesSource(2001, "\r", false), 1},
		{"U+2028 breaks a line", maxLinesSource(2001, "\u2028", false), 1},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, testCase.source, atLimit)
			if testCase.findings == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, "exceed")
		})
	}
}

// The finding reports the count and runs from the first line past the maximum to the end, upstream's
// location.
func TestMaxLinesReportsFromTheFirstLinePastTheMaximum(test *testing.T) {
	test.Parallel()

	source := "first;\nsecond;\nthird;\nfourth;\n"
	result := rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, source, MaxLinesOptions{Maximum: 2})
	rule_testing.ExpectFindings(test, result, "exceed")
	finding := result.Diagnostics[0]
	if reported := source[finding.Range.Pos():finding.Range.End()]; reported != "third;\nfourth;\n" {
		test.Errorf("reported %q, want from the third line to the end", reported)
	}
	if !strings.Contains(finding.Message.Description, "File has too many lines (4). Maximum allowed is 2.") {
		test.Errorf("message %q does not carry upstream's count and maximum", finding.Message.Description)
	}
}

// The default is upstream's 300, and the two skip options drop exactly the lines upstream drops.
func TestMaxLinesOptions(test *testing.T) {
	test.Parallel()

	test.Run("the default is 300", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, rule_testing.Run(test, MaxLines, maxLinesFile, maxLinesSource(300, "\n", true)))
		rule_testing.ExpectFindings(test, rule_testing.Run(test, MaxLines, maxLinesFile, maxLinesSource(301, "\n", true)), "exceed")
	})

	blankLines := "first;\n\n   \nsecond;\n"
	test.Run("blank lines count unless skipped", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, blankLines,
			MaxLinesOptions{Maximum: 2}), "exceed")
		rule_testing.ExpectClean(test, rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, blankLines,
			MaxLinesOptions{Maximum: 2, SkipBlankLines: true}))
	})

	// Three comment-only lines (a line comment, and the first and last lines of a block comment with
	// one more inside it), and two lines where code shares a line with a comment, which still count.
	commentLines := "// only a comment\n/*\n inside\n*/\nconst kept = 1; // trailing\n/* leading */ const also = 2;\n"
	test.Run("comment-only lines count unless skipped", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, commentLines,
			MaxLinesOptions{Maximum: 2}), "exceed")
		rule_testing.ExpectClean(test, rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, commentLines,
			MaxLinesOptions{Maximum: 2, SkipComments: true}))
		rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, MaxLines, maxLinesFile, commentLines,
			MaxLinesOptions{Maximum: 1, SkipComments: true}), "exceed")
	})
}

func TestMaxLinesDecodesUpstreamsShapes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		raw  string
		want MaxLinesOptions
	}{
		{"", MaxLinesOptions{Maximum: 300}},
		{"2000", MaxLinesOptions{Maximum: 2000}},
		{`{"max": 2000}`, MaxLinesOptions{Maximum: 2000}},
		{`{"skipBlankLines": true}`, MaxLinesOptions{Maximum: 300, SkipBlankLines: true}},
		{`{"max": 10, "skipComments": true}`, MaxLinesOptions{Maximum: 10, SkipComments: true}},
	}
	for _, testCase := range cases {
		decoded, err := DecodeMaxLinesOptions([]byte(testCase.raw))
		if err != nil {
			test.Errorf("%q: %v", testCase.raw, err)
			continue
		}
		options, ok := decoded.(MaxLinesOptions)
		if !ok {
			test.Errorf("%q decoded to %T, not MaxLinesOptions", testCase.raw, decoded)
			continue
		}
		if options != testCase.want {
			test.Errorf("%q decoded to %+v, want %+v", testCase.raw, decoded, testCase.want)
		}
	}
	if _, err := DecodeMaxLinesOptions([]byte("-1")); err == nil {
		test.Error("a negative maximum was accepted")
	}
}

// TestMaxLinesEachSplitsAsTheSliceSplitterDid pins maxLinesEach to the answers of the slice splitter
// it replaced (#smshtp5): every line's start and end, without its break, for each line break and for
// the edges a counter gets wrong, the empty text, a missing final break, a final break, and blank
// lines in a row. The old splitter's offsets were checked against these rows before it was removed,
// alongside 200,000 random texts.
func TestMaxLinesEachSplitsAsTheSliceSplitterDid(test *testing.T) {
	test.Parallel()
	lineSeparator := string(rune(0x2028))
	paragraphSeparator := string(rune(0x2029))
	for _, testCase := range []struct {
		name  string
		text  string
		lines [][2]int
	}{
		{"empty text is one empty line", "", [][2]int{{0, 0}}},
		{"one line with no final break", "a", [][2]int{{0, 1}}},
		{"a final line feed ends the line rather than starting another", "a\n", [][2]int{{0, 1}}},
		{"a lone line feed is one empty line", "\n", [][2]int{{0, 0}}},
		{"line feed", "a\nb", [][2]int{{0, 1}, {2, 3}}},
		{"carriage return and line feed are one break", "a\r\nb", [][2]int{{0, 1}, {3, 4}}},
		{"a final carriage return and line feed", "a\r\n", [][2]int{{0, 1}}},
		{"a lone carriage return", "a\rb", [][2]int{{0, 1}, {2, 3}}},
		{"line separator", "a" + lineSeparator + "b", [][2]int{{0, 1}, {4, 5}}},
		{"paragraph separator", "a" + paragraphSeparator + "b", [][2]int{{0, 1}, {4, 5}}},
		{"blank lines in a row", "a\n\n\nb", [][2]int{{0, 1}, {2, 2}, {3, 3}, {4, 5}}},
		{"a blank line before a final break", "a\n\n", [][2]int{{0, 1}, {2, 2}}},
		{"two breaks and nothing else", "\r\n\r\n", [][2]int{{0, 0}, {2, 2}}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			var lines [][2]int
			for start, end := range maxLinesEach(testCase.text) {
				lines = append(lines, [2]int{start, end})
			}
			if !slices.Equal(lines, testCase.lines) {
				test.Errorf("%q splits into %v, want %v", testCase.text, lines, testCase.lines)
			}
		})
	}
}
