package consistency_no_utils_file_name_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_utils_file_name"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_utils_file_name.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-utils-file-name"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on the first line of each file the rule must report, at the file's start: a
// name that is the word, one holding it after an underscore, a test file, whose _test is set aside, and the word
// capitalized, whose house spelling keeps its case. The allowed fixtures prove the rule stays quiet on the house
// spelling, on a name that runs the word into another, and on a file named for what it holds; the generated and
// third_party fixtures, on names that are not the project's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_utils_file_name.Analyzer,
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
