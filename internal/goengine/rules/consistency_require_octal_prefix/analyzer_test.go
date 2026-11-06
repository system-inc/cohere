package consistency_require_octal_prefix_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_require_octal_prefix"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_octal_prefix.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-octal-prefix")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixture carries a // want on every legacy octal literal, naming the literal and its suggestion, and
// analysistest fails on a finding no want expects as much as on a want nothing found. Its .golden file beside it is
// the source with every offered fix applied, so a fix that writes the wrong digits fails even when the finding is
// right. The allowed and lookalike fixtures prove the rule stays quiet on zero, the prefixed bases, decimals, floats
// and imaginary literals that open with a zero, and octal digits in strings, runes and comments.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_octal_prefix.Analyzer,
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
