package testing_no_uncached_run_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/testing_no_uncached_run"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := testing_no_uncached_run.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "testing-no-uncached-run"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want at every -count=1 the rule must report, inline, spread from a literal, traced
// through a local or appended, and analysistest fails on a finding no want expects as much as on a want nothing found,
// so the allowed and lookalike fixtures prove the rule stays quiet on a cached go test, a slice it cannot read, a
// local rewritten after its declaration, a program go run runs, and an exec lookalike from another package.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		testing_no_uncached_run.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
		"lookalike/exec",
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
