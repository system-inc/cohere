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

// The verdict counts a build's new candidate for an unanswered Interface lookup only where the rule could take it as
// fixing the name, and leaves out a concrete type's method with an unexported name when our method is not an
// interface's (methodLookupHolds, #swv3jbw). That rests on foreignInterfaces indexing interfaces only, so no concrete
// method of another package fixes a name. unexportedmethods reaches an unexported concrete type's parseURL with the
// name and signature of a concrete type's parseURL and a generic type's of ours, and both stay findings, so a rule
// that let such a method fix either by its name and signature fails on its want.
func TestAnUnexportedConcreteForeignMethodNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_uppercase_initialism.Analyzer,
		"unexportedmethods")
}

// The verdict's method index leaves out an alias of a type that is not an interface (#swv3jbw), and this rule reads
// interfaces alone, so an alias of a concrete type fixes no name here. An alias of a named type always reaches a
// package already in the closure, which declares the same methods. aliasedconcrete reaches foreign.Runner, an alias
// of a concrete type with HomeURL, and a HomeURL of ours stays a finding, so a rule that read a concrete alias's
// methods would fail on its want.
func TestAnAliasOfAConcreteTypeNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_uppercase_initialism.Analyzer,
		"aliasedconcrete")
}

// An alias of an interface stays in the verdict's method index (#swv3jbw) because this rule reads one: build passes
// every type name to add, and add reads through an alias to its interface, which may have no other name.
// aliasedinterface reaches foreign.Located, an alias of an interface written in place, and place, which implements it,
// keeps its HomeURL silent, while shop, which implements nothing, stays a finding. A rule that skipped aliases fails on
// place, and the verdict would then index more than the rule reads, which is sound but no longer the rule's own.
func TestAnAliasOfAnInterfaceFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_uppercase_initialism.Analyzer,
		"aliasedinterface")
}

// The verdict's method index reads an interface's whole method set and a concrete type's declared methods only
// (#swv3jbw). This rule reads interfaces alone, so its part of that is two things: an interface's methods are its
// whole method set, embedded ones included, and a concrete type fixes no name, by a method it declares or one
// promoted to it. promotedmethods' place implements foreign.Located, which has HomeURL only by embedding an interface
// written in place, and its HomeURL stays silent; its shop has the HomeURL foreign.Runner has by promotion, and stays
// a finding. A rule that read an interface's explicit methods alone fails on place, and one that read a concrete
// type's method set fails on shop.
func TestOnlyDeclaredMethodsFixAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_uppercase_initialism.Analyzer,
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
