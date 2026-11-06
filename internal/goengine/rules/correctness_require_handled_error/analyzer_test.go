package correctness_require_handled_error_test

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/correctness_require_handled_error"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := correctness_require_handled_error.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "correctness-require-handled-error")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every call that drops an error, as a statement, under defer or go, or into
// _, and analysistest fails on a finding no want expects as much as on a want nothing found, so the allowed and
// lookalike fixtures prove the rule stays quiet on a handled error, a reasoned marker, a write exempt by its writer's
// type, and a call whose results only look like an error.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		correctness_require_handled_error.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
}

// crypto/rand.Read is exempt only from Go 1.24, and the version is the package's, so each side is a module of its own
// under testdata/src: analysistest loads a directory holding a go.mod in module mode, where go.mod's go line is the
// package's version. olderversion, at go 1.23, flags Read; newerversion, at go 1.24, leaves it alone even in a file
// whose //go:build line lowers the file's language to go1.23, since the toolchain is still 1.24 or later.
func TestAnalyzerReadsThePackageGoVersion(test *testing.T) {
	test.Parallel()
	for _, module := range []string{"olderversion", "newerversion"} {
		directory := filepath.Join(analysistest.TestData(), "src", module)
		analysistest.Run(errorsOnly{test}, directory, correctness_require_handled_error.Analyzer, "./...")
	}
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
