package testing_require_failure_path_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/testing_require_failure_path"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := testing_require_failure_path.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "testing-require-failure-path"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every test that cannot fail on a wrong result, and analysistest fails on a
// finding no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule
// stays quiet on a test that fails through testing's methods, a panic, a helper or reporter it hands the test to, or
// a skip before anything runs, and on functions that only look like tests.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		testing_require_failure_path.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
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
