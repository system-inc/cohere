package consistency_no_any_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_any"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := consistency_no_any.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	if _, parseError := policy.Naming.ParseRuleName("cohere-go", "consistency-no-any"); parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every any and interface{} the rule must report, in parameters, results,
// fields, interface methods, variables, named types and type arguments, and analysistest fails on a finding no want
// expects as much as on a want nothing found. So the allowed fixtures prove the rule stays quiet where another
// package fixes the type: a method matching another package's, a callback, a forwarded slice, a value taken from
// another package; and the lookalike fixtures prove it stays quiet on constraints, types inside expressions, and a
// local type or parameter named any.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		consistency_no_any.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
}

// The verdict counts a build's new candidate for an unanswered Method lookup only where the rule could take it as
// fixing the name, and leaves out a concrete type's method with an unexported name when our method is not an
// interface's (methodLookupHolds, #swv3jbw). That rests on foreignMethods.add indexing exported methods only.
// unexportedmethods reaches an unexported concrete type's store with the name and signature of a concrete type's store
// and a generic type's of ours, and both anys stay findings, so a rule that let such a method fix either fails on its
// want.
func TestAnUnexportedConcreteForeignMethodNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_any.Analyzer, "unexportedmethods")
}

// The verdict's method index leaves out an alias of a type that is not an interface, and buildForeignMethods skips
// every alias (#swv3jbw). An alias of a named type adds nothing, since that type's package is in the closure and
// declares the same methods, so the alias that matters names a concrete type written in place, a struct with a method
// only by promotion. aliasedconcrete reaches foreign.Vault, such an alias, the only type carrying Store, and the any
// of a Store of ours with its signature stays a finding, so a rule that read a concrete alias's method set would fail
// on its want.
func TestAnAliasOfAConcreteTypeNeverFixesAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_any.Analyzer, "aliasedconcrete")
}

// The verdict's method index reads a concrete type's declared methods only, as foreignMethods.add does (#swv3jbw). A
// promoted method comes from an embedded field, and an embedded named type is declared in a package of the closure,
// which declares the method itself, so promotion adds a method no package declares only from an embedded alias of an
// interface written in place. promotedmethods reaches foreign.Vault, which has Store by that promotion alone, and the
// any of a Store of ours with its signature stays a finding, so a rule that read Vault's method set would fail on its
// want.
func TestOnlyDeclaredMethodsFixAName(test *testing.T) {
	test.Parallel()
	analysistest.Run(errorsOnly{test}, analysistest.TestData(), consistency_no_any.Analyzer, "promotedmethods")
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
