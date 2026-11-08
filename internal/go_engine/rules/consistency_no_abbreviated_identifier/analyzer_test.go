package consistency_no_abbreviated_identifier_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_abbreviated_identifier.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-abbreviated-identifier")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every kind of name Go declares, in every form the vocabulary judges, with
// the descriptive advice where the bare expansion is already Go's. analysistest fails on a finding no want expects as
// much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays quiet on full words,
// uses, imported package names, embedded fields, and methods another package's interface or type fixes.
//
// flagged.go.golden is flagged.go with every offered rename applied to its declaring identifier, the engine's
// part, renaming each reference, left out. So it shows exactly which findings offer a rename and to what: a whole
// word, a prefix, a suffix, the millisecond word and a segment in both cases, each to its one spelling, and `ctx`, a
// context parameter, to its caller's name. `err`, a named result, and `str`, whose advice asks for a descriptive name,
// a word with several meanings, and an advice-style word offer none.
//
// errorcalls.go.golden is the same for local errors named err: one written once by a single call is told the name
// that call gives it and offered it, a table call, a function value and a declared function, while one written twice,
// one whose name is taken there or in a scope nested in it, a constructed error, one whose address is taken, and an
// err that isn't an error keep the advice and offer nothing.
//
// contextcalls.go.golden is the same for contexts named ctx, matched by type: a parameter, a method's among them, is
// offered callerContext and a local from context.WithCancel cancelContext, while one written twice, one from a call
// outside the table, one whose name is taken there or in a nested scope, a parameter the body writes, and a ctx that
// is a string or a type of ours named Context keep the advice and offer nothing.
//
// namesake reaches a package named foreign that is not the one lookalike reaches, and has lookalike's Cmd where nothing
// it reaches fixes Cmd. What each package declares is read once a load and shared, so a memo that told packages apart
// by name, or gave a package what another reaches, would leave namesake's Cmd alone or flag lookalike's, whichever
// package the load read first.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_abbreviated_identifier.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
		"errorcalls",
		"contextcalls",
		"namesake",
	)
}

// renameExports is the engine's to read, so turning it on changes no finding and no rename the rule offers: the
// same fixtures, wants and golden pass with it set.
func TestRenameExportsChangesNoFinding(test *testing.T) {
	test.Parallel()
	analyzer := consistency_no_abbreviated_identifier.NewAnalyzer()
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
		"lookalike", "errorcalls", "contextcalls", "namesake")
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
