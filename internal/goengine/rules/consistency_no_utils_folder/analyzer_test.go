package consistency_no_utils_folder_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/system-inc/cohere/internal/goengine/rules/consistency_no_utils_folder"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_utils_folder.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-utils-folder"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want at the package clause of each package the rule must report: folders named
// utils, util and _utils, a utils folder nested below another, a utils clause in a folder named otherwise, and a
// package whose every file is generated, reported at its first. The utils package also holds a second file, an
// in-package test and an external test, and the util package opens with a generated file, so a finding anywhere but
// the first written file, or a second finding for one package, fails. The allowed and lookalike fixtures prove the
// rule stays quiet on the house spelling, on names that only contain utils, and on a file, import or variable named
// utils. The packages are named one by one, since a ... pattern skips a folder whose name begins with an underscore.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_utils_folder.Analyzer,
		"flagged/utils",
		"flagged/util",
		"flagged/_utils",
		"flagged/clause",
		"flagged/nested/utils",
		"flagged/generated/utils",
		"allowed/utilities",
		"allowed/plain",
		"lookalike/utilsomething",
		"lookalike/myutils",
		"lookalike/mention",
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
