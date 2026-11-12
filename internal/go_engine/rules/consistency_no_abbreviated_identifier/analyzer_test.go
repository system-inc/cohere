package consistency_no_abbreviated_identifier_test

import (
	"slices"
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

// A word the vocabulary gives more than one meaning, or advice in place of one, is found and never renamed. An audit
// of the fix's renames found single expansions that guessed: `res` became `response` where it meant a result or
// MakeMap's reserve, `ch` and `dir` meant a rune and a ChanDir, `Var` meant JavaScript's var keyword, and `pred`,
// go/ir's predecessor, would have renamed a filter's predicate. So ambiguous.go declares each in every sense the
// audit found, `pred` as a predicate, and its golden keeps every one of those names.
//
// analysistest compares a golden only for a file some fix edits, so a rule that offered no rename at all would pass
// the golden by skipping it. The test reads the renames themselves instead: exactly one, `strValue`'s, the word with
// one meaning the file declares beside them, so it fails as much on a fix that never runs as on one that guesses.
func TestAWordWithSeveralMeaningsIsNeverRenamed(test *testing.T) {
	test.Parallel()
	results := analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_abbreviated_identifier.Analyzer,
		"ambiguous",
	)
	var renames []string
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			for _, fix := range diagnostic.SuggestedFixes {
				renames = append(renames, fix.Message)
			}
		}
	}
	want := []string{`Rename "strValue" to "stringValue" at its declaration and at every reference.`}
	if !slices.Equal(renames, want) {
		test.Errorf("renames %q, want %q", renames, want)
	}
}

// The abbreviations go/ast, go/types and go/ir taught Go code are the house's words to spell out (#r0fa8pa,
// #2xt6v5r), judged only where the word ends: treewords.go declares `expr`, `condExpression`, `fset` and
// `visitorFunc`, each renamed to its one spelling, `typ`, whose expansion is Go's keyword, and `recv` and `op`, which
// mean more than one thing, beside a struct of full words that open with the same letters (`Condition`, `Channel`,
// `Function`, `Selection`, `Typical`, `Optional`) and carry no want, so a vocabulary that read `Cond` inside
// `Condition` fails on a finding no want expects. The renames are read as well, so `typ`, `recv` and `op` offering
// one fails too.
func TestTheSyntaxTreeWordsAreJudgedWhereTheWordEnds(test *testing.T) {
	test.Parallel()
	results := analysistest.RunWithSuggestedFixes(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_abbreviated_identifier.Analyzer,
		"treewords",
	)
	var renames []string
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			for _, fix := range diagnostic.SuggestedFixes {
				renames = append(renames, fix.Message)
			}
		}
	}
	want := []string{
		`Rename "expr" to "expression" at its declaration and at every reference.`,
		`Rename "condExpression" to "conditionExpression" at its declaration and at every reference.`,
		`Rename "fset" to "fileSet" at its declaration and at every reference.`,
		`Rename "visitorFunc" to "visitorFunction" at its declaration and at every reference.`,
	}
	if !slices.Equal(renames, want) {
		test.Errorf("renames %q, want %q", renames, want)
	}
}

// JavaScript's var keyword is a word, not short for "variable", so a Go name holding one of policy/KeywordTerms.json's
// spellings for it reads clean, as the TypeScript and Swift rules read it (#hvyvfr5). keywordterms.go declares them
// beside a bare `Var`, a spelling run on into a longer word, and a keyword term next to another abbreviation or a bare
// `Var`, each of which still carries a want, and NoVar is the control, so a rule that judged nothing would fail on
// its wants.
func TestAKeywordTermReadsAsAWord(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_abbreviated_identifier.Analyzer,
		"keywordterms",
	)
	findings := 0
	for _, result := range results {
		findings += len(result.Diagnostics)
	}
	if findings != 5 {
		test.Errorf("%d findings, want the five the fixture's wants name", findings)
	}
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

// The verdict counts a build's new candidate for an unanswered Method lookup only where the rule could take it as
// fixing the name, and leaves out a concrete type's method with an unexported name when our method is not an
// interface's (methodLookupHolds, #swv3jbw). That rests on fixingMethod letting a concrete foreign method fix only an
// interface method's name. unexportedmethods reaches an unexported concrete type's cmd with the name and signature of
// a concrete type's cmd and a generic type's of ours, and both stay findings, so a rule that let such a method fix
// either name fails on its want.
func TestAnUnexportedConcreteForeignMethodNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_abbreviated_identifier.Analyzer,
		"unexportedmethods")
}

// The verdict's method index leaves out an alias, as declaredMethodsOf does (#swv3jbw). An alias of a named type
// adds nothing, since that type's package is in the closure and declares the same methods, so the alias that matters
// names an interface written in place, which no named type declares. aliasedmethods reaches only such an alias
// carrying Cmd, and its Cmd on a concrete type that has all of it and on an interface of its own stay findings, so a
// rule that read an alias's interface would fail on their wants.
func TestAnAliasNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_abbreviated_identifier.Analyzer,
		"aliasedmethods")
}

// The verdict's method index reads a concrete type's declared methods only, as declaredMethodsOf does (#swv3jbw). A
// promoted method comes from an embedded field, and an embedded named type is declared in a package of the closure,
// which declares the method itself, so promotion adds a method no package declares only from an embedded alias of an
// interface written in place. promotedmethods reaches foreign.Runner, which has Cmd by that promotion alone, and an
// interface method of ours with its signature stays a finding, so a rule that read Runner's method set, where any
// foreign method of the signature fixes an interface method's name, would fail on its want.
func TestOnlyDeclaredMethodsFixAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_abbreviated_identifier.Analyzer,
		"promotedmethods")
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
