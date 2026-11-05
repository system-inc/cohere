package concurrency_no_buffered_wait_delay_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/concurrency_no_buffered_wait_delay"
	"github.com/system-inc/cohere/policy"
)

// fixtureCure is the import path of the stand-in cure package under testdata/src.
const fixtureCure = "cure/childguard"

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := concurrency_no_buffered_wait_delay.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "concurrency-no-buffered-wait-delay")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// With the cure configured, the flagged fixtures and the calls into the cure carry a // want on every WaitDelay the
// rule must report, and analysistest fails on a finding no want expects as much as on a want nothing found, so the
// allowed, lookalike and cure fixtures prove the rule stays quiet on file outputs, a reasoned marker, a field that
// is not exec's, a lookalike of the cure at another path, and the cure package itself.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analyzer := concurrency_no_buffered_wait_delay.NewAnalyzer()
	if setError := analyzer.Flags.Set("cure", fixtureCure); setError != nil {
		test.Fatal(setError)
	}
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		analyzer,
		"flagged",
		"allowed",
		"lookalike",
		"lookalike/childguard",
		"curecalls",
		fixtureCure,
	)
}

// With the cure option left empty, the rule is off: the unconfigured fixture holds shapes the configured rule flags
// and no want, so any finding there fails the test.
func TestAnalyzerIsSilentWithoutACure(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		concurrency_no_buffered_wait_delay.NewAnalyzer(),
		"unconfigured",
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
