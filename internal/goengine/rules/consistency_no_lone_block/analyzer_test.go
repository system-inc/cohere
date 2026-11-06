package consistency_no_lone_block_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_lone_block"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_lone_block.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-lone-block"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every bare block that declares nothing directly inside it: an empty one,
// one holding plain statements or a defer, one in a case, a select case, a function literal, a generic function and
// a test file, one whose only declarations sit in an if's init, a for's clause, a nested block or a function literal,
// and the outer of two nested blocks. analysistest fails on a finding no want expects as much as on a want nothing
// found, so the allowed and lookalike fixtures prove the rule stays quiet on a bare block that declares with :=, var,
// const or type, a labeled block, and the braces the syntax requires: function, if, else, for, switch, select and
// literal bodies, composite literals and empty struct and interface types.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_lone_block.Analyzer,
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
