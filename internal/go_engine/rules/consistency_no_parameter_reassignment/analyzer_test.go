package consistency_no_parameter_reassignment_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_parameter_reassignment"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_parameter_reassignment.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName(
		"cohere-go",
		"consistency-no-parameter-reassignment",
	); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want on every reassignment the rule must report: =, op=, a := that reuses the
// parameter, a range with =, ++, --, a receiver reassigned, an outer parameter reassigned from a function literal,
// and &parameter. analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed
// and lookalike fixtures prove the rule stays quiet on writes through a parameter, named results, a shadowing := in
// an inner scope, and a local or package variable of a parameter's name.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_parameter_reassignment.Analyzer,
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
