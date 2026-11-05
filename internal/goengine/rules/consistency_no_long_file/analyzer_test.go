package consistency_no_long_file_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_long_file.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-long-file"); parseError != nil {
		test.Fatal(parseError)
	}
}

// At the house limit, the flagged fixture of 2,001 lines carries a want at the start of its last line, and
// analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and lookalike
// fixtures prove the rule stays quiet on a file of exactly 2,000 lines ending in a newline, one of 2,000 with no
// final newline, a generated file of 3,000, and a short file a //line directive numbers past the limit.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_long_file.NewAnalyzer(),
		"flagged",
		"allowed",
		"lookalike",
	)
}

// With the limit set to 50, a file of 51 lines is flagged at its 51st and its neighbour of exactly 50 is not, so the
// option is read from the analyzer running the pass and a stricter limit moves the finding with it.
func TestAnalyzerWithAStricterLimit(test *testing.T) {
	test.Parallel()
	analyzer := consistency_no_long_file.NewAnalyzer()
	if setError := analyzer.Flags.Set("limit", "50"); setError != nil {
		test.Fatal(setError)
	}
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), analyzer, "stricter")
}

// A limit that is not a whole number of lines, one or more, is refused when it is set, so a bad setting fails the
// engine's configuration instead of flagging every file or naming a line no file has.
func TestLimitRefusesWhatIsNotALineCount(test *testing.T) {
	test.Parallel()
	for _, text := range []string{"0", "-1", "", "many", "2000.5"} {
		analyzer := consistency_no_long_file.NewAnalyzer()
		if setError := analyzer.Flags.Set("limit", text); setError == nil {
			test.Errorf("setting the limit to %q was accepted", text)
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
