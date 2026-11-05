package consistency_no_long_line_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_long_line"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_long_line.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-long-line"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a want for every line past 120 columns that could wrap: code, a string after code or
// before more code, a keyed or named string with code or a comment around it, a comment of words, a line of 121 with
// tabs, and a field with a func type, two names, two type arguments, a trailing comment or a tag past which it
// overruns. analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and
// lookalike fixtures prove the rule stays quiet on what cannot wrap (an import path, a //go: directive, a raw string's
// body, a lone string, a string after only a key or a name and its operator, a URL too long for a line of its own, a
// parameter, result or struct field of one name and a type gofmt cannot break) and on a line of exactly 120 with tabs
// or past 120 only in bytes.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_long_line.Analyzer,
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
