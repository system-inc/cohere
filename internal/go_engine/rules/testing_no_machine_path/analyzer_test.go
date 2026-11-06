package testing_no_machine_path_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/testing_no_machine_path"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := testing_no_machine_path.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "testing-no-machine-path"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The fixtures carry each home directory the rule must find, in a plain, raw or longer string, and each marker that
// excuses nothing as flagged: one with no reason, mid-sentence, in the wrong case, with its reason on the next line,
// trailing the line above, or a blank line away.
// The flagged fixtures carry a // want on every literal the rule must report, and analysistest fails on a finding no
// want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet
// on a reasoned marker, a literal outside a _test.go file, and text that only resembles a home directory.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		testing_no_machine_path.Analyzer,
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
