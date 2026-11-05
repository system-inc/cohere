package consistency_no_property_alias_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_property_alias"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_property_alias.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-property-alias"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want on every alias the rule must report: := and var, a deep chain, a promoted field,
// a pointer receiver, a package variable's field, a generic struct, a function literal's own local, an if's init, a
// range expression, a deferred call's argument, a multi-byte name, and a read after harmless work. analysistest fails
// on a finding no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the
// rule stays quiet on reaches written directly, different names, package scope, and every alias
// that may be a snapshot: read twice, in a closure or a loop, written through, after a call or a write, after a
// shadowing declaration, beside a goto, or through a chain holding a call, an index, a method or a package name.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_property_alias.Analyzer,
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
