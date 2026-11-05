package consistency_no_shouting_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_shouting"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_shouting.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-shouting"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every comment the rule must report, naming the tokens it must name, and
// analysistest fails on a finding no want expects as much as on a want nothing found. The TypeScript rule's own test
// cases sit in the fixtures as Go comments, in both directions, to show the engines agree on the same text. A
// generated file is read like any other, so a shout past its marker is flagged, and so is the marker's line after the
// package clause, where it marks nothing. The allowed and lookalike fixtures prove the rule stays quiet on acronyms,
// masked code, Go's directives, notes, example output and generated-code marker, and capitals that are not in a
// comment at all.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_shouting.Analyzer,
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
