package consistency_no_redundant_import_alias_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_redundant_import_alias"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_redundant_import_alias.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-redundant-import-alias")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every alias that repeats both the package's name and its path's last
// element: alone and in a group, on a nested path, after multi-byte runes, before a comment, in a generated file and
// in an external test. analysistest fails on a finding no want expects as much as on a want nothing found, and each
// .golden file beside a flagged file is that file with every offered fix applied, so a fix that eats a comment or
// leaves the alias fails even when the finding is right. The allowed and lookalike fixtures prove the rule stays quiet
// on plain, blank and dot imports, a real rename, an alias that names a package its path does not spell (a /v2 path,
// a folder named go-widget or yaml.v3), an alias that spells the path but not the package's name, and an alias that
// differs from the name only in case.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_redundant_import_alias.Analyzer,
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
