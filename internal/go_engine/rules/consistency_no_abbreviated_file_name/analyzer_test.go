package consistency_no_abbreviated_file_name_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_file_name"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_abbreviated_file_name.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-abbreviated-file-name")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on the first line of each file the rule must report, at the file's start: a
// name that is the word, one whose first word is, a test file whose last word is once its _test is set aside, a word
// with more than one meaning, a word with no expansion, which gives its entry's advice, and a word beside a registered
// rule's name. The allowed fixtures prove the rule stays quiet on names spelled in full, a test file's included, and on
// a registered rule's name, alone or beside a word in full; the generated and third_party fixtures, on names that are
// not the project's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_abbreviated_file_name.Analyzer,
		"flagged",
		"allowed",
		"generated",
		"third_party/library",
	)
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
