package consistency_require_matching_file_name_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_require_matching_file_name"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_matching_file_name.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-matching-file-name")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every file whose one type is not its name: a declaration in a file named
// for something else or for a theme, a file of methods named for neither the type nor a theme of it, a generic type,
// a type whose only method sits in a test file, main.go outside package main, a build tag's word on a file with no
// //go:build line, two words on one with it, and a file named for its interface whose one real type sits beside
// marker-only variants. analysistest fails on a finding no want expects as much as on a want nothing found, so the
// allowed and lookalike fixtures prove the rule stays quiet on a file named for its type in any spelling of its words,
// a build-constrained variant named for its tag, a themed file of methods, a //line directive, a test file's helper,
// a file serving no type or two, a real type named for beside marker-only variants, a file of only an interface and
// its marker variant, and main.go in package main.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_matching_file_name.Analyzer,
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
