package security_no_interpolated_shell_command_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/security_no_interpolated_shell_command"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := security_no_interpolated_shell_command.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "security-no-interpolated-shell-command")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every script the rule must report, and analysistest fails on a finding no
// want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet
// on a value passed as the shell's own argument, a number or a boolean spliced in, a script of constants, a shell
// run without -c, and a Command or Sprintf that is not os/exec's or fmt's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		security_no_interpolated_shell_command.Analyzer,
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
