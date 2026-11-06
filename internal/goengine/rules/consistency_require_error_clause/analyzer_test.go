package consistency_require_error_clause_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_require_error_clause"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_error_clause.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-error-clause")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want for every problem the rule must report, one per message id, and analysistest
// fails on a finding no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove
// the rule stays quiet on a clean clause, every proper-name exemption, a text that is no literal, and a call that is
// not the standard library's errors.New or fmt.Errorf.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_error_clause.Analyzer,
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
