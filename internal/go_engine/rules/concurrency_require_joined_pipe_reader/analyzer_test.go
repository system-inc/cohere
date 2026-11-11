package concurrency_require_joined_pipe_reader_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/concurrency_require_joined_pipe_reader"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := concurrency_require_joined_pipe_reader.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "concurrency-require-joined-pipe-reader")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every wait some path reaches from a goroutine reading a pipe before
// anything joins it: os/exec's own go io.Copy, a literal with nothing to join it by, a WaitGroup waited on after the
// child or only by a defer, a channel received on one branch, Run, a WaitGroup's Go joined late, and a reader writing
// through another package's interface. analysistest fails on a finding no want expects as much as on a want nothing
// found, so the allowed and lookalike fixtures prove the rule stays quiet on a join by a WaitGroup, a channel's close,
// a result received or a select, a WaitGroup's Go waited on, a signal handed to a helper or forwarded by another
// goroutine, a reader calling into this package or holding a mutex or a context, a channel reached through a field,
// a pipe read inline, a goroutine that never touches the pipe, another command's wait, no wait, and a type that is
// not os/exec's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		concurrency_require_joined_pipe_reader.Analyzer,
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
