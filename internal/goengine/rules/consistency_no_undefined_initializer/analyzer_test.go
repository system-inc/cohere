package consistency_no_undefined_initializer_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_undefined_initializer"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_undefined_initializer.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-undefined-initializer")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixture carries a // want on every typed var whose initializer restates its zero value, and
// analysistest fails on a finding no want expects as much as on a want nothing found. Its .golden file beside it is
// the source with every offered fix applied, so a fix that removes too much or too little fails even when the finding
// is right, and a spec with a comment inside its initializer keeps it, since no fix is offered there. The allowed and
// lookalike fixtures prove the rule stays quiet on a var with no type, a short declaration, a non-zero value, a call,
// a composite literal, a zero boxed in an interface, a shadowed nil or false, and the spellings it declines.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_undefined_initializer.Analyzer,
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
