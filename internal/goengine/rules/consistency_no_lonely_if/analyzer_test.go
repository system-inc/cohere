package consistency_no_lonely_if_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_lonely_if"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_lonely_if.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-lonely-if"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every if that is the only statement of an else block: a plain one, one with
// its own else or else-if chain, one in the final else of a chain, one nested in another's else, one with an init
// statement, one with a comment above or after it inside the block, and in a function literal, a generic function
// and a line with runes of more than one byte. analysistest fails on a finding no want expects as much as on a want
// nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on an else-if, an else holding
// more than the if, an empty else, a lone if in a then branch, a case clause or a function body, a labeled if, and an
// if held by a nested block, a loop or a function literal inside the else.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_lonely_if.Analyzer,
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
