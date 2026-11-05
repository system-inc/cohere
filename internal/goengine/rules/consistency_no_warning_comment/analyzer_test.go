package consistency_no_warning_comment_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_warning_comment"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_warning_comment.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-warning-comment"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every line the rule must report, one pattern per word, and analysistest
// fails on a finding no want expects as much as on a want nothing found. The allowed and lookalike fixtures prove the
// rule stays quiet on directives, words in backticks, a generated file, the words in another case or inside a longer
// word, and the words outside a comment. analysistest checks only lines, so every finding's range is also read back
// from the source and must be the word itself.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_warning_comment.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
	requireFindingsOnTheirWords(test, results)
}

// A CRLF file's block comment reaches the rule with its carriage returns gone, so its text is shorter than its source
// after the first line. The word opens its line, where a position drifted back by those bytes would land on the line
// before and miss the want. The file is written here because the repository's .gitattributes stores every text file
// with LF endings, so a committed CRLF fixture would arrive as LF.
func TestAnalyzerFindsTheWordInACrlfBlockComment(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	source := strings.Join([]string{
		"package crlf",
		"",
		"// want +5 `\"FIXME\"`",
		"/*",
		"A block comment in a CRLF file,",
		"its lines ending in a carriage return and a newline,",
		"and the word at the start of one below the first:",
		"FIXME the drift",
		"*/",
		"var value = 1",
		"",
	}, "\r\n")
	packageDirectory := filepath.Join(root, "src", "crlf")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	fileName := filepath.Join(packageDirectory, "crlf.go")
	if writeError := os.WriteFile(fileName, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	results := analysistest.Run(errorsOnly{test}, root, consistency_no_warning_comment.Analyzer, "crlf")
	requireFindingsOnTheirWords(test, results)
}

// requireFindingsOnTheirWords reads each finding's range back from its file and requires it to be one of the words,
// since the rule reports at the word and not at the comment.
func requireFindingsOnTheirWords(test *testing.T, results []*analysistest.Result) {
	test.Helper()
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			tokenFile := result.Pass.Fset.File(diagnostic.Pos)
			content, readError := os.ReadFile(tokenFile.Name())
			if readError != nil {
				test.Fatal(readError)
			}
			reported := string(content[tokenFile.Offset(diagnostic.Pos):tokenFile.Offset(diagnostic.End)])
			if reported != "TODO" && reported != "FIXME" && reported != "XXX" {
				position := result.Pass.Fset.Position(diagnostic.Pos)
				test.Errorf("%s: the finding covers %q, not the word", position, reported)
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
