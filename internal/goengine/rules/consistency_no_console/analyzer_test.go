package consistency_no_console_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_console"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_console.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-console"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want at every write to standard output the rule must report: fmt's three, the
// builtins, the standard logger's nine, fmt's three writes to a writer handed os.Stdout or os.Stderr, under an import
// alias or a dot import, handed on as a value, and behind a //line directive naming a test file. analysistest fails
// on a finding no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the
// rule stays quiet in package main, in a test file, on a write to a writer the caller hands in or through a
// *log.Logger, on os.Stderr given as a value rather than the writer, and on a print, Println or Stdout that resolves
// to something else.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_console.Analyzer,
		"flagged",
		"allowed",
		"allowed/command",
		"lookalike",
		"lookalike/fmt",
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
