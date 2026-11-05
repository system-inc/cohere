package consistency_no_dash_punctuation_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_dash_punctuation"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_dash_punctuation.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-dash-punctuation"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want for every dash: in a doc comment, a line comment, a block comment on one line and
// across lines, an interpreted string, a raw string on one line and across lines, and spelled by \u, \U, \x and octal
// escapes. analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and
// lookalike fixtures prove the rule stays quiet on a hyphen-minus, the minus sign, the Unicode hyphen and a rune
// literal. Each finding's span is then checked against the dash it names.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_dash_punctuation.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
	checkReportedSpans(test, results)
}

// Go's scanner drops every carriage return from a block comment's and a raw string's text, so in a file with CRLF
// line endings a dash on a later line sits further into the file than its offset into that text says. The flagged
// fixture is run again with CRLF endings to prove each finding still lands on the dash itself. The copy is written
// at test time because the repository's .gitattributes keeps every committed file LF.
func TestAnalyzerWithCarriageReturns(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "flagged", "flagged.go"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	packageDirectory := filepath.Join(directory, "src", "flagged")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	carriageReturnSource := bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n"))
	writeError := os.WriteFile(filepath.Join(packageDirectory, "flagged.go"), carriageReturnSource, 0o644)
	if writeError != nil {
		test.Fatal(writeError)
	}
	results := analysistest.Run(errorsOnly{test}, directory, consistency_no_dash_punctuation.Analyzer, "flagged")
	checkReportedSpans(test, results)
}

// checkReportedSpans reads each finding's own bytes back from its file, since a want matches only a line: a written
// dash must be reported exactly at that dash, and an escaped one at the whole literal.
func checkReportedSpans(test *testing.T, results []*analysistest.Result) {
	test.Helper()
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			// The file's own offsets, not ones a //line directive would give, since the bytes are read from it.
			start := result.Pass.Fset.PositionFor(diagnostic.Pos, false)
			end := result.Pass.Fset.PositionFor(diagnostic.End, false)
			source, readError := os.ReadFile(start.Filename)
			if readError != nil {
				test.Fatal(readError)
			}
			reported := string(source[start.Offset:end.Offset])
			switch diagnostic.Category {
			case "dashPunctuation":
				character, size := utf8.DecodeRuneInString(reported)
				if (character != '\u2014' && character != '\u2013') || size != len(reported) {
					test.Errorf("%s: a written dash reported over %q, not the dash alone", start, reported)
				}
			case "escapedDashPunctuation":
				if len(reported) < 2 || reported[0] != '"' || reported[len(reported)-1] != '"' {
					test.Errorf("%s: an escaped dash reported over %q, not the whole literal", start, reported)
				}
			default:
				test.Errorf("%s: unknown category %q", start, diagnostic.Category)
			}
		}
	}
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
