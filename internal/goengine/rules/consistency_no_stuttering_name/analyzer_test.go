package consistency_no_stuttering_name_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_stuttering_name"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_stuttering_name.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-stuttering-name"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want on every stutter the rule must report: a field or method, exported or not,
// reached through a local, a parameter, a receiver, a range or closure variable, a package variable, a variable of a
// type parameter, and through parentheses or a dereference. analysistest fails on a finding no want expects as much
// as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on generic names doing
// their job, a stutter on a word off the list, a member chain, a map key, an imported package, a type's method
// expression, a dot-imported variable, and a generated file or a variable a generated file declares.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_stuttering_name.Analyzer,
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
