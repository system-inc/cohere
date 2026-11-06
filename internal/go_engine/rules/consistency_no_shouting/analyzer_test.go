package consistency_no_shouting_test

import (
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_shouting"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_shouting.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-shouting"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every comment the rule must report, naming the tokens it must name, and
// analysistest fails on a finding no want expects as much as on a want nothing found. The TypeScript rule's own test
// cases sit in the fixtures as Go comments, in both directions, to show the engines agree on the same text. A
// generated file is read like any other, so a shout past its marker is flagged, and so is the marker's line after the
// package clause, where it marks nothing. The allowed and lookalike fixtures prove the rule stays quiet on acronyms,
// masked code, Go's directives, notes, example output and generated-code marker, and capitals that are not in a
// comment at all.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_shouting.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
}

// Each finding covers the word its message names, byte for byte, which the wants above cannot see: analysistest
// matches a finding's line and message, never its columns. The flagged fixtures put words after multi-byte text,
// inside masked code and out, on a block comment's lines below its opening one, and twice in one comment, so a finding
// off by a rune's extra bytes, at the comment's start, or at a word's first occurrence covers something else.
func TestEachFindingCoversItsWord(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_shouting.Analyzer, "flagged")
	sources := map[string][]byte{}
	checked := 0
	for _, result := range results {
		// Per result, since the package and its test variant are each a result that reads flagged.go.
		reportedAt := map[token.Pos]bool{}
		for _, diagnostic := range result.Diagnostics {
			file := result.Pass.Fset.File(diagnostic.Pos)
			source, isRead := sources[file.Name()]
			if !isRead {
				var readError error
				source, readError = os.ReadFile(file.Name())
				if readError != nil {
					test.Fatalf("reading the fixture %s: %v", file.Name(), readError)
				}
				sources[file.Name()] = source
			}
			position := result.Pass.Fset.Position(diagnostic.Pos)
			covered := string(source[file.Offset(diagnostic.Pos):file.Offset(diagnostic.End)])
			if !strings.HasPrefix(diagnostic.Message, `"`+covered+`" in a comment`) {
				test.Errorf("%s: the finding covers %q, and its message is %q", position, covered, diagnostic.Message)
			}
			if reportedAt[diagnostic.Pos] {
				test.Errorf("%s: two findings start here, so one word was reported for another", position)
			}
			reportedAt[diagnostic.Pos] = true
			checked++
		}
	}
	if checked == 0 {
		test.Fatal("the flagged fixtures gave no findings, so no span was checked")
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
