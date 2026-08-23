package typescript

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const assertedOptionalChainFile = "/repository/source/Thing.ts"

// The cases below are the original's own test file plus a run of the real plugin at 8.67.0, which
// is the only answer key this rule has: the tree verify gates contains zero non-null assertions, so
// a clean run over it cannot distinguish a working port from an inert one.
func TestNoNonNullAssertedOptionalChainFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"an assertion ending a member chain", "declare const foo: any;\nexport const r = foo?.bar!;\n"},
		{"an assertion ending a call chain", "declare const foo: any;\nexport const r = foo?.bar()!;\n"},
		{"an assertion ending an element chain", "declare const foo: any;\nexport const r = foo?.[0]!;\n"},
		{"an assertion after an optional call", "declare const foo: any;\nexport const r = foo.bar?.()!;\n"},
		{"an assertion ending a longer chain", "declare const foo: any;\nexport const r = foo?.bar.baz!;\n"},
		// The parenthesized forms. Parens invert which node wraps which, so these reach the rule
		// through the second condition rather than the first. They are the half a single-condition
		// port loses, and they fail silently.
		{"an assertion wrapping a parenthesized chain", "declare const foo: any;\nexport const r = (foo?.bar)!;\n"},
		{"a parenthesized chain then a member", "declare const foo: any;\nexport const r = (foo?.bar)!.baz;\n"},
		{"a parenthesized chain then a call", "declare const foo: any;\nexport const r = (foo?.bar)!();\n"},
		{"a parenthesized assertion at chain end", "declare const foo: any;\nexport const r = (foo?.bar!);\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t,
				ruletest.Run(t, NoNonNullAssertedOptionalChain, assertedOptionalChainFile, testCase.sourceText),
				"noNonNullOptionalChain")
		})
	}
}

func TestNoNonNullAssertedOptionalChainStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		// The two cases the whole rule turns on. Both contain an assertion directly after an
		// optional access, which is the shape a naive port reports, and both are valid because the
		// chain continues past the assertion so its short-circuit still reaches the result. These
		// are the fixtures that sit exactly where the rule decides.
		{"an assertion mid-chain before a member", "declare const foo: any;\nexport const r = foo?.bar!.baz;\n"},
		{"an assertion mid-chain before a call", "declare const foo: any;\nexport const r = foo?.bar!();\n"},
		{"an assertion mid-chain before an element access", "declare const foo: any;\nexport const r = foo?.bar![0];\n"},
		{"an assertion mid-chain, longer", "declare const foo: any;\nexport const r = foo?.bar!.baz.qux;\n"},
		// An assertion inside the chain's brackets rather than on the chain: the optional link does
		// not reach through a computed property's own expression.
		{"an assertion inside a computed property", "declare const foo: any;\ndeclare const key: any;\nexport const r = foo?.[key!];\n"},
		{"an assertion in an argument", "declare const foo: any;\ndeclare const x: any;\nexport const r = foo?.bar(x!);\n"},
		// An assertion with no chain at all, and a chain with no assertion: each half of the
		// condition present without the other.
		{"an assertion with no chain", "declare const foo: any;\nexport const r = foo!;\n"},
		{"an assertion then a plain member", "declare const foo: any;\nexport const r = foo!.bar;\n"},
		{"a doubled assertion with no chain", "declare const foo: any;\nexport const r = foo!!;\n"},
		{"a chain with no assertion", "declare const foo: any;\nexport const r = foo?.bar;\n"},
		{"a chain in a parenthesis with no assertion", "declare const foo: any;\nexport const r = (foo?.bar);\n"},
		// An assertion before the chain rather than after it: `foo!?.bar` asserts on `foo`, and the
		// `?.` that follows does its own check. Redundant, which is the other rule's business, and
		// not this rule's.
		{"an assertion before an optional access", "declare const foo: any;\nexport const r = foo!?.bar;\n"},
		{"a definite assignment", "export let value!: number;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t,
				ruletest.Run(t, NoNonNullAssertedOptionalChain, assertedOptionalChainFile, testCase.sourceText))
		})
	}
}
