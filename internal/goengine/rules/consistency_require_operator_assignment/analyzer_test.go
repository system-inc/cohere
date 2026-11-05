package consistency_require_operator_assignment_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_require_operator_assignment"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_operator_assignment.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-operator-assignment")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every assignment that reads its own target where the compound form would,
// for each of the eleven operators and for variables, fields, elements, dereferences and another package's variable,
// and analysistest fails on a finding no want expects as much as on a want nothing found. Each .golden file is its
// source with every offered fix applied, so a fix that writes the wrong operator or drops a parenthesis fails even
// when the finding is right, and the reversed commutative form and the spans holding a comment stay as written there
// because they carry no fix. The allowed and lookalike fixtures prove the rule stays quiet on the compound form
// itself, a reversed non-commutative operator, string concatenation reversed, a regrouped chain, a key that is a
// call or arithmetic, and two spellings that name different places.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_operator_assignment.Analyzer,
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
