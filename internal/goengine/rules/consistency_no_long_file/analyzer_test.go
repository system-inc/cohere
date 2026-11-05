package consistency_no_long_file_test

import (
	"strings"
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
// final newline, and a short file a //line directive numbers past the limit.
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

// The limit may only make the rule stricter, so 2,001 is refused with an error naming the option and the value,
// which the engine prints as the run's refusal, while 1,500 and the house limit itself are taken and read back.
func TestLimitRefusesALooserLimit(test *testing.T) {
	test.Parallel()
	looser := consistency_no_long_file.NewAnalyzer()
	setError := looser.Flags.Set("limit", "2001")
	if setError == nil {
		test.Fatal("setting the limit to 2001 was accepted")
	}
	if !strings.Contains(setError.Error(), "limit option of 2001 lines") {
		test.Errorf("the refusal does not name the option and the value: %v", setError)
	}
	for _, text := range []string{"1500", "2000"} {
		stricter := consistency_no_long_file.NewAnalyzer()
		if setError := stricter.Flags.Set("limit", text); setError != nil {
			test.Fatalf("setting the limit to %s was refused: %v", text, setError)
		}
		if read := stricter.Flags.Lookup("limit").Value.String(); read != text {
			test.Errorf("the limit set to %s reads back as %s", text, read)
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
