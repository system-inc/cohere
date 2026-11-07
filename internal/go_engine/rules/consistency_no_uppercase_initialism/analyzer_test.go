package consistency_no_uppercase_initialism_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_uppercase_initialism.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-uppercase-initialism")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want, with the suggested name, on every kind of declaration that spells an
// initialism in capitals, and analysistest fails on a finding no want expects as much as on a want nothing found, so
// the allowed and lookalike fixtures prove the rule stays quiet on names spelled as words, on uses and package names,
// on methods an interface of another package fixes, on directive names and examples, on one capital before a word,
// and on screaming snake case.
//
// Each .golden file beside a flagged file is that file with every offered rename applied to its declaring
// identifier, the engine's part, renaming each reference, left out. Every finding offers one, to the name its
// message suggests, so where a golden and a want disagree, the fix and the finding name two spellings.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_uppercase_initialism.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
}

// renameExports is the engine's to read, so turning it on changes no finding and no rename the rule offers: the
// same fixtures, wants and goldens pass with it set.
func TestRenameExportsChangesNoFinding(test *testing.T) {
	test.Parallel()
	analyzer := consistency_no_uppercase_initialism.NewAnalyzer()
	if rule.RenamesExports(analyzer) {
		test.Fatal("renameExports is on by default, so the fix would rename exports nobody asked it to")
	}
	if setError := analyzer.Flags.Set(rule.RenameExportsOption, "true"); setError != nil {
		test.Fatal(setError)
	}
	if !rule.RenamesExports(analyzer) {
		test.Fatal("renameExports set to true reads as off")
	}
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), analyzer, "flagged", "allowed",
		"lookalike")
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
