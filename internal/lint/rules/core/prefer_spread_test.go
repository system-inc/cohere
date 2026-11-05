package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestPreferSpreadReportsRedundantThisArgument is the fixture that must fire.
//
// The two shapes the rule is about: a bare callee given an empty `this`, and a member callee given
// back the object it was reached through.
func TestPreferSpreadReportsRedundantThisArgument(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"foo.apply(undefined, args);",
		"foo.apply(null, args);",
		"foo.apply(void 0, args);",
		"obj.foo.apply(obj, args);",
		"a.b.c.foo.apply(a.b.c, args);",
		"a.b(x, y).c.foo.apply(a.b(x, y).c, args);",
		"a.b().c.foo.apply(a.b().c, args);",
		"getFn().apply(undefined, args);",
		"outer(inner(x)).m.apply(outer(inner(x)), args);",
		"class C { m(args: any) { this.foo.apply(this, args); } }",
		"class C { #foo; m(args: any) { obj.#foo.apply(obj, args); } }",
		"wrap(foo.apply(null, args));",
	} {
		result := rule_testing.Run(test, PreferSpread, "apply.ts", source)
		rule_testing.ExpectFindings(test, result, "preferSpread")
	}
}

// TestPreferSpreadReadsBothMemberAccessForms pins that a computed access to a literal `apply` is
// the same call as a dotted one.
//
// `foo['apply'](null, args)` does exactly what `foo.apply(null, args)` does, so a rule that read
// only the dotted form would have a hole that any minifier output falls into.
func TestPreferSpreadReadsBothMemberAccessForms(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		`foo["apply"](null, args);`,
		"foo[`apply`](null, args);",
		`obj["foo"].apply(obj, args);`,
		`obj["foo"].apply(obj["foo"] && obj, args);`,
	} {
		result := rule_testing.Run(test, PreferSpread, "computed.ts", source)
		if source == `obj["foo"].apply(obj["foo"] && obj, args);` {
			rule_testing.ExpectClean(test, result)
			continue
		}
		rule_testing.ExpectFindings(test, result, "preferSpread")
	}
}

// TestPreferSpreadIgnoresCommentsAndWhitespace pins the token oracle.
//
// The comparison is over tokens, so a receiver written across three lines with a comment inside it
// is the same receiver as one written inline. A comparison over raw source text would miss every
// one of these, which is the shape real formatted code takes.
func TestPreferSpreadIgnoresCommentsAndWhitespace(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"obj\n  .foo\n  .apply(obj, args);",
		"obj /* x */ . foo . apply(obj, args);",
		"[].concat.apply([ ], args);",
		"[].concat.apply([\n/* comment */\n], args);",
		"[1, 2].concat.apply([1, 2], args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "trivia.ts", source)
		rule_testing.ExpectFindings(test, result, "preferSpread")
	}
}

// TestPreferSpreadSeesThroughGroupingParentheses pins where parentheses are transparent.
//
// Parentheses wrapping the whole callee, the whole receiver, or an argument are pure grouping and
// change nothing, so they must not hide the call. This is separate from the optional-chaining case
// below, where the parentheses do change meaning.
func TestPreferSpreadSeesThroughGroupingParentheses(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"foo.apply((null), args);",
		"obj.foo.apply((obj), args);",
		"foo.apply(null, (args));",
		"(foo.apply)(null, args);",
		"(obj.foo).apply(obj, args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "parens.ts", source)
		rule_testing.ExpectFindings(test, result, "preferSpread")
	}
}

// TestPreferSpreadHandlesOptionalChaining pins the shapes that still report.
//
// An optional call or an optional property read still ends in the same `.apply()` with the same
// redundant `this`, so the rewrite is still available and the rule still fires.
func TestPreferSpreadHandlesOptionalChaining(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"foo.apply?.(undefined, args);",
		"foo?.apply(undefined, args);",
		"foo?.apply?.(undefined, args);",
		"(foo?.apply)(undefined, args);",
		"a?.b.c.foo.apply(a?.b.c, args);",
		"(a?.b).c.foo.apply((a?.b).c, args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "optional.ts", source)
		rule_testing.ExpectFindings(test, result, "preferSpread")
	}
}

// TestPreferSpreadKeepsParenthesizedOptionalChainsDistinct is the documented divergence from ESLint.
//
// `(a?.b).c` throws when `a` is null and `a?.b.c` short-circuits to undefined, so they are not the
// same expression. ESLint's AST has no node for the parentheses and reports these as matching
// receivers; ours keeps the node and stays silent, which is the stricter and the correct reading.
// A finding here would be a false one.
func TestPreferSpreadKeepsParenthesizedOptionalChainsDistinct(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"(a?.b).c.foo.apply(a?.b.c, args);",
		"a?.b.c.foo.apply((a?.b).c, args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "divergence.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestPreferSpreadStaysSilentWhenTheThisBindingMatters is the half that catches a rule firing on
// correct code.
//
// Every case here is an `.apply()` that is doing the job `.apply()` exists for: setting a `this`
// the call would not otherwise have. Rewriting any of them to spread syntax would change behavior.
func TestPreferSpreadStaysSilentWhenTheThisBindingMatters(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"foo.apply(obj, args);",
		"obj.foo.apply(null, args);",
		"obj.foo.apply(undefined, args);",
		"obj.foo.apply(otherObj, args);",
		"a.b(x, y).c.foo.apply(a.b(x, z).c, args);",
		"a.b.foo.apply(a.b.c, args);",
		"obj.foo.apply(this, args);",
		"class C { m(args: any) { this.foo.apply(that, args); } }",
		"class C extends B { m(args: any) { super.foo.apply(this, args); } }",
		"(obj as any).foo.apply(obj, args);",
		"[1, 2].concat.apply([1, 3], args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "binding.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestPreferSpreadLeavesArrayLiteralsToNoUselessCall pins the boundary between two rules.
//
// Building an array only to take it apart is a real defect and it is `no-useless-call`'s. Reporting
// it here would give the reader a message about `this` bindings for a call whose `this` is not the
// problem.
func TestPreferSpreadLeavesArrayLiteralsToNoUselessCall(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"foo.apply(undefined, [1, 2]);",
		"foo.apply(null, [1, 2]);",
		"obj.foo.apply(obj, [1, 2]);",
		"foo.apply(null, ([1, 2]));",
		"obj.foo.apply(obj, ...args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "literals.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}

// TestPreferSpreadIgnoresCallsThatAreNotApply pins what the rule declines to read.
//
// A computed key that is not a literal cannot be resolved without knowing what the variable holds,
// and a private `#apply` is a different property that spells the same word. Both must stay silent,
// and both are shapes a looser check would report.
func TestPreferSpreadIgnoresCallsThatAreNotApply(test *testing.T) {
	test.Parallel()

	for _, source := range []string{
		"var apply; foo[apply](null, args);",
		"class C { #apply; m() { foo.#apply(undefined, args); } }",
		"foo.apply();",
		"obj.foo.apply();",
		"foo.apply(null);",
		"foo.apply(null, args, extra);",
		"foo.call(null, args);",
		"foo(...args);",
		"obj.foo(...args);",
	} {
		result := rule_testing.Run(test, PreferSpread, "other.ts", source)
		rule_testing.ExpectClean(test, result)
	}
}
