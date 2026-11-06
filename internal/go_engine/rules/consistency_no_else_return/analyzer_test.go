package consistency_no_else_return_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_else_return"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_else_return.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-else-return"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every else the rule must report: after a plain return, after a chain whose
// every branch returns, after a branch that returns through a nested block or a fully returning if, and in a case
// clause, a select clause, a function literal and a generic function. analysistest fails on a finding no want expects
// as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on an early
// return with no else, a branch that can fall through, a chain with no final else, an else that reads an init
// statement's variable, a labeled if, and a branch that ends in a panic, a continue, a goto, a
// switch, or a return that belongs to a function literal.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_else_return.Analyzer,
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
