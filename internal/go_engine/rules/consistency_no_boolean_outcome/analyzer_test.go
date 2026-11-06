package consistency_no_boolean_outcome_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_boolean_outcome"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_boolean_outcome.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-boolean-outcome"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want on every flag the rule must report: each of the twins' flag names, exported or
// not, beside each companion, in a struct declared at package scope, in a function, as an alias or a generic, with an
// embedded companion, a flag typed through an alias of bool, and in a generated file. analysistest fails on a finding
// no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet
// on a named outcome, a flag with no companion, the twins' third-party and display-state exemptions, (value, ok) and
// (value, error) results, a flag or companion off the lists, a flag that is not the predeclared bool, a promoted
// companion, and a struct spelled inline.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_boolean_outcome.Analyzer,
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
