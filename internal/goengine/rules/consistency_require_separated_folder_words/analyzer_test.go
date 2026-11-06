package consistency_require_separated_folder_words_test

import (
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/consistency_require_separated_folder_words"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_require_separated_folder_words.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-require-separated-folder-words")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want at the package clause the rule must report, and nowhere else. formatoptions
// is reported once for its package, at formatoptions.go, though an in-package test comes before it by name and a
// second file after it, and once for its external test package, at that package's first file, all its files being
// tests. jsonlines opens with a generated file and a test, so its finding moves to lines.go, the first file that is
// neither. goengine, regexsyntax and cgotest split on a two-letter word, a software term and the three-letter minimum,
// imessagebridge on a brand that is a software term too, runoutput by the tie rule, and text_formatoptions in one of
// its parts. clause is a word whose package clause differs from it and runs words together. The allowed fixtures are
// words a split would break, a name already separated, names that split no way, and a command whose clause is main.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_require_separated_folder_words.Analyzer,
		"flagged/formatoptions",
		"flagged/goengine",
		"flagged/regexsyntax",
		"flagged/cgotest",
		"flagged/imessagebridge",
		"flagged/runoutput",
		"flagged/text_formatoptions",
		"flagged/clause",
		"flagged/jsonlines",
		"allowed/workspace",
		"allowed/charset",
		"allowed/gitignore",
		"allowed/changelog",
		"allowed/lowercase",
		"allowed/format_options",
		"allowed/cachedvfs",
		"allowed/acheck",
		"allowed/withc",
		"allowed/command",
	)
}

// The module fixture is a module named example.com/goengine, so its packages are judged below the module root: the
// root package is clean though the module's own path runs words together, inner is a word, and treebuilder inside it
// is reported.
func TestAnalyzerJudgesBelowTheModuleRoot(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		filepath.Join(analysistest.TestData(), "module"),
		consistency_require_separated_folder_words.Analyzer,
		"example.com/goengine",
		"example.com/goengine/inner",
		"example.com/goengine/inner/treebuilder",
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
