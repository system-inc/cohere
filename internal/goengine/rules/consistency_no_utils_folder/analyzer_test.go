package consistency_no_utils_folder_test

import (
	"path/filepath"
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

// The flagged fixtures carry a // want on the first line of each file the rule must report, at the file's start: every
// file of folders named utils, util and _utils, in any casing, whether the folder is the package's own or one above
// it, and every file whose package clause is utils, util or Utils in a folder named otherwise. The utils package
// holds a second file, an in-package test and an external test, judged by its folder though its path ends in
// utils_test, the util package a generated file, and the clause package a second file, so a file without its finding,
// or with a second, fails. The allowed and lookalike fixtures prove the rule stays quiet on the house spelling in any
// casing, on names that only contain utils, above the package or as its own folder, on a folder named utils_test that
// holds no external test, and on a file, import or variable named utils. The packages are named one by one, since a
// ... pattern skips a folder whose name begins with an underscore.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_utils_folder.Analyzer,
		"flagged/utils",
		"flagged/util",
		"flagged/_utils",
		"flagged/_Util",
		"flagged/upper/Utils",
		"flagged/middle/util/inner",
		"flagged/clause",
		"flagged/clausecase",
		"flagged/clauseunderscores",
		"flagged/nested/utils",
		"flagged/generated/utils",
		"allowed/utilities",
		"allowed/plain",
		"lookalike/utilsomething",
		"lookalike/myutils",
		"lookalike/myutils/inner",
		"lookalike/upper/Utilities",
		"lookalike/_Utilities",
		"lookalike/clausecase",
		"lookalike/utils_test",
		"lookalike/mention",
	)
}

// The module fixture is a module named example.com/utils, so its packages are judged below the module root: the root
// package and inner are clean though the module's own path ends in utils, and a Util folder inside it is reported.
func TestAnalyzerJudgesBelowTheModuleRoot(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		filepath.Join(analysistest.TestData(), "module"),
		consistency_no_utils_folder.Analyzer,
		"example.com/utils",
		"example.com/utils/inner",
		"example.com/utils/inner/Util",
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
