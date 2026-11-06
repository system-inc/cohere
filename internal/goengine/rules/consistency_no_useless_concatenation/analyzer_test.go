package consistency_no_useless_concatenation_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_useless_concatenation"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_useless_concatenation.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-useless-concatenation")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every + the rule must report: two interpreted or two raw literals, a chain
// whose outer + stands beside two literals, parenthesized operands, a constant, a named string type, a generic
// function, a comment or a /*line*/ directive between the operands, a raw literal ending on the operator's line, and a
// raw and an interpreted literal whose texts paste into one. analysistest fails on a finding no want expects as much
// as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on a join with anything
// but a literal, a join wrapped across lines or around a //line directive, rune and number arithmetic, a +=, and a raw
// and an interpreted literal that one literal could only hold with new escapes.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_useless_concatenation.Analyzer,
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
