package correctness_require_drained_child_output_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/correctness_require_drained_child_output"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := correctness_require_drained_child_output.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "correctness-require-drained-child-output")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every wait some path reaches from a pipe's creation with nothing reading
// the pipe: Servers.app's shape, a drain on one branch, Run, a stderr pipe, a pipe never read, a deferred Close, a
// loop whose read follows its wait, and a pipe made in a loop or a function literal. analysistest fails on a finding
// no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays
// quiet on 5c5833e's fixed form, a goroutine's drain, a drain on every branch, a scanner's loop, a stream that is no
// pipe, a pipe handed on or stored, no wait, a command or pipe written again, a deferred wait, and a type or pipe
// that is not os/exec's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		correctness_require_drained_child_output.Analyzer,
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
