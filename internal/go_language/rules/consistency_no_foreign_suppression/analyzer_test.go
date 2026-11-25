package consistency_no_foreign_suppression_test

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	// The registry marks this rule a syntax-only walk as it initializes, wrapping Analyzer in place so it runs on a
	// pass without type information (rules.syntaxOnlyWalks), and only a test binary that links the registry runs
	// the fixtures through that wrap, which is what shows the rule reads nothing the mark forbids.
	_ "github.com/system-inc/cohere/internal/go_language/rules"
	"github.com/system-inc/cohere/internal/go_language/rules/consistency_no_foreign_suppression"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_foreign_suppression.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName(
		"cohere-go",
		"consistency-no-foreign-suppression",
	); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every directive the rule must report: staticcheck's //lint:ignore,
// //lint:file-ignore and a malformed //lint: command, and golangci-lint's //nolint:name on its own line, trailing code,
// naming several checks, and bare, both trailing and on its own line, in a test file and in a generated one, which
// the rule judges and the driver withholds. analysistest fails on a finding no want expects as much as on a want
// nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on a //cohere:ignore, the directives
// inside a raw string and an interpreted one, a mention of them in prose or after a comment's opening space, a block
// comment, and words that only start like the directives. analysistest checks only lines, so every finding's range is
// also read back from the source and must be the directive's first word.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_foreign_suppression.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			tokenFile := result.Pass.Fset.File(diagnostic.Pos)
			content, readError := os.ReadFile(tokenFile.Name())
			if readError != nil {
				test.Fatal(readError)
			}
			reported := string(content[tokenFile.Offset(diagnostic.Pos):tokenFile.Offset(diagnostic.End)])
			isDirective := strings.HasPrefix(reported, "//lint:") || strings.HasPrefix(reported, "//nolint")
			if !isDirective || strings.ContainsAny(reported, " \t\n") {
				test.Errorf("%s: the finding covers %q, not the directive's first word",
					result.Pass.Fset.Position(diagnostic.Pos), reported)
			}
		}
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
