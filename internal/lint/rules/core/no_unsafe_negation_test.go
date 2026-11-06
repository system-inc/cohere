package core

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// unsafeNegationFile is where the fixtures pretend to live.
const unsafeNegationFile = "/repository/source/UnsafeNegation.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_unsafe_negation.rs`:
// one Tester block, 18 pass and 12 fail, and the snapshot records 12 diagnostics from those 12 fail
// inputs, so one finding per input is measured here rather than assumed. Pulled with the
// extractor's own Rust parser rather than transcribed by eye, because the options tuple is the half
// a transcription drops and an options case read as a defaults case asserts nothing about the option.
//
// The option is `enforceForOrderingRelations`, default false. Note that the same four inputs appear
// on both sides of the corpus, differing only in whether the option is set: `if (! a < b) {}` is
// clean by default and reports with the option on. That pairing is the whole reason the option
// cases have to carry their options.
func TestNoUnsafeNegationFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		options    rule.Given[NoUnsafeNegationOptions]
	}{
		{"in", "!a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"in inside parentheses", "(!a in b)", rule.Given[NoUnsafeNegationOptions]{}},
		{"in with a parenthesised operand", "!(a) in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"instanceof", "!a instanceof b", rule.Given[NoUnsafeNegationOptions]{}},
		{"instanceof inside parentheses", "(!a instanceof b)", rule.Given[NoUnsafeNegationOptions]{}},
		{"instanceof with a parenthesised operand", "!(a) instanceof b", rule.Given[NoUnsafeNegationOptions]{}},
		// A regex literal as the negated operand. A naive tokenizer lexes the leading `/` as
		// division and never sees a binary `in` here at all.
		{"a regex operand", "(y=>{if(!/s/ in(l)){}})", rule.Given[NoUnsafeNegationOptions]{}},
		{
			"less than, option on",
			"if (! a < b) {}",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"greater than, option on",
			"while (! a > b) {}",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"less or equal, option on",
			"foo = ! a <= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"greater or equal, option on",
			"foo = ! a >= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"less or equal bare, option on",
			"! a <= b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test,
				rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile, testCase.sourceText,
					testCase.options), "unexpected")
		})
	}
}

// The clean cases carry the two distinctions this rule turns on.
//
// The first is parenthesisation of the left operand, and it is the case that punishes the house
// reflex. `(!a) in b` is already unambiguous: the author wrote the parentheses to say the negation
// applies to `a` alone, and reporting it would be telling them their explicit grouping is a
// mistake. Our AST keeps a ParenthesizedExpression node there, so the kind check declines it with
// no paren logic at all, and `ast.SkipParentheses` (which ten rules in this package reach for)
// would unwrap it into a false positive. Verified by probing our parser rather than reasoned about:
// `(!a) in b` yields KindParenthesizedExpression on the left, `!(a) in b` yields
// KindPrefixUnaryExpression, and those two go opposite ways in the corpus.
//
// The second is the option gate. The four ordering operators are silent by default, including under
// an explicitly empty options object, and including under an explicit false.
func TestNoUnsafeNegationStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		options    rule.Given[NoUnsafeNegationOptions]
	}{
		{"a plain in", "a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"in compared to false", "a in b === false", rule.Given[NoUnsafeNegationOptions]{}},
		{"the whole in negated", "!(a in b)", rule.Given[NoUnsafeNegationOptions]{}},
		{"a parenthesised negation before in", "(!a) in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"a plain instanceof", "a instanceof b", rule.Given[NoUnsafeNegationOptions]{}},
		{"instanceof compared to false", "a instanceof b === false", rule.Given[NoUnsafeNegationOptions]{}},
		{"the whole instanceof negated", "!(a instanceof b)", rule.Given[NoUnsafeNegationOptions]{}},
		{"a parenthesised negation before instanceof", "(!a) instanceof b", rule.Given[NoUnsafeNegationOptions]{}},
		{"less than, option off by default", "if (! a < b) {}", rule.Given[NoUnsafeNegationOptions]{}},
		{"greater than, option off by default", "while (! a > b) {}", rule.Given[NoUnsafeNegationOptions]{}},
		{"less or equal, option off by default", "foo = ! a <= b;", rule.Given[NoUnsafeNegationOptions]{}},
		{"greater or equal, option off by default", "foo = ! a >= b;", rule.Given[NoUnsafeNegationOptions]{}},
		// An empty options object still has to default the option to false, which a decoder that
		// treated "present but unset" as true would get wrong.
		{"less or equal, empty options object", "! a <= b", rule.Configured(NoUnsafeNegationOptions{})},
		{
			"greater or equal, option explicitly off",
			"foo = ! a >= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: false}),
		},
		{
			"a parenthesised negation before an ordering operator",
			"foo = (!a) >= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"an ordering comparison with no negation",
			"a <= b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"the whole ordering comparison negated",
			"!(a < b)",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{
			"greater than with no negation",
			"foo = a > b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile, testCase.sourceText,
					testCase.options))
		})
	}
}

// Cases upstream does not cover, each written from reading our own code.
//
// The corpus never exercises an equality operator, so nothing in it distinguishes "the operators
// this rule cares about" from "any binary operator". `!a === b` is a real and common shape, it is
// not this rule's business, and a port whose operator test was inverted or absent would report it
// while passing all 30 upstream cases.
//
// Nor does the corpus exercise any prefix operator other than `!`. `-a in b` and `~a in b` parse to
// the same PrefixUnaryExpression node kind and differ only in the operator token, so a port that
// checked the node kind and forgot the token would report both.
//
// `typeof a === "x"` is the shape a reader worries about most when they hear "negation before a
// relational operator", and `typeof` is a prefix unary too. It is exempt for both reasons at once.
func TestNoUnsafeNegationDeclinesOtherOperatorsAndOtherUnaries(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		options    rule.Given[NoUnsafeNegationOptions]
	}{
		{"strict equality is not relational", "!a === b", rule.Given[NoUnsafeNegationOptions]{}},
		{"loose equality is not relational", "!a == b", rule.Given[NoUnsafeNegationOptions]{}},
		{"strict inequality is not relational", "!a !== b", rule.Given[NoUnsafeNegationOptions]{}},
		{"addition is not relational", "!a + b", rule.Given[NoUnsafeNegationOptions]{}},
		{
			"equality stays out with the option on",
			"!a === b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
		{"unary minus is not a logical not", "-a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"bitwise not is not a logical not", "~a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"unary plus is not a logical not", "+a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"typeof is not a logical not", "typeof a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"void is not a logical not", "void a in b", rule.Given[NoUnsafeNegationOptions]{}},
		{"a double negation before an ordering operator, option off", "!!a < b", rule.Given[NoUnsafeNegationOptions]{}},
		// The negation on the right operand is not the hazard: `a in !b` groups as written.
		{"a negation on the right operand", "a in !b", rule.Given[NoUnsafeNegationOptions]{}},
		{
			"a negation on the right of an ordering operator",
			"a < !b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile, testCase.sourceText,
					testCase.options))
		})
	}
}

// A double negation is still a negation of the left operand, and it reports once rather than twice.
//
// `!!a in b` parses as one binary expression whose left is a PrefixUnaryExpression whose operand is
// another. Only the outer one is the left operand of the `in`, so a rule listening on the binary
// expression reports once by construction. Pinned because a port that listened on the unary instead
// and walked up to its parent would report twice here, and the message-id fixtures above use inputs
// where the two designs agree.
func TestNoUnsafeNegationReportsADoubleNegationOnce(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectFindings(test,
		rule_testing.Run(test, NoUnsafeNegation, unsafeNegationFile, "!!a in b"), "unexpected")
}

// Where the finding points, which the message-id fixtures above cannot see.
//
// Upstream labels the left operand rather than the whole comparison or the `!` token alone, and the
// snapshot's carets are the record of it: `!a in b` underlines two columns from column 1, and
// `(y=>{if(!/s/ in(l)){}})` underlines four from column 9. Asserted by slicing the source with the
// finding's own range, because a span checked against a number computed the same way the rule
// computed it agrees with itself.
//
// `! a <= b` is the case that catches an anchor on the `!` token alone: the operand is a space away,
// so the correct span is three characters and a token-only span is one.
func TestNoUnsafeNegationPointsAtTheNegatedOperand(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		options    rule.Given[NoUnsafeNegationOptions]
		want       string
	}{
		{"in", "!a in b", rule.Given[NoUnsafeNegationOptions]{}, "!a"},
		{"inside parentheses", "(!a in b)", rule.Given[NoUnsafeNegationOptions]{}, "!a"},
		{"a parenthesised operand", "!(a) in b", rule.Given[NoUnsafeNegationOptions]{}, "!(a)"},
		{"instanceof", "!a instanceof b", rule.Given[NoUnsafeNegationOptions]{}, "!a"},
		{"a regex operand", "(y=>{if(!/s/ in(l)){}})", rule.Given[NoUnsafeNegationOptions]{}, "!/s/"},
		{
			"a space after the bang",
			"! a <= b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"! a",
		},
		{
			"inside an if",
			"if (! a < b) {}",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"! a",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile,
				testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one diagnostic, got %d", len(result.Diagnostics))
			}
			reported := testCase.sourceText[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
			if reported != testCase.want {
				test.Fatalf("the finding points at %q, wanted %q", reported, testCase.want)
			}
		})
	}
}

// The repairs, applied rather than compared as text.
//
// Two suggestions and no fix, which is a deliberate divergence from oxc and is argued at length on
// the rule. The first rewrite is upstream's `expect_fix` column verbatim, so the corpus still checks
// our first suggestion even though we refuse to apply it unattended.
//
// Applied here rather than compared against the fix's own text, because a repair writing the right
// string over the wrong span passes a text comparison. `(!a in b)` is the case that needs it: the
// correct rewrite touches only the inner binary and leaves the outer parentheses alone, and a repair
// that replaced one character too many either way would still carry the text `!(a in b)`.
func TestNoUnsafeNegationSuggestsBothRewrites(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name         string
		sourceText   string
		options      rule.Given[NoUnsafeNegationOptions]
		wantNegated  string
		wantExplicit string
	}{
		// The first six are upstream's fix column verbatim, in its order.
		{"in", "!a in b", rule.Given[NoUnsafeNegationOptions]{}, "!(a in b)", "(!a) in b"},
		{"inside parentheses", "(!a in b)", rule.Given[NoUnsafeNegationOptions]{}, "(!(a in b))", "((!a) in b)"},
		{"a parenthesised operand", "!(a) in b", rule.Given[NoUnsafeNegationOptions]{}, "!((a) in b)", "(!(a)) in b"},
		{
			"instanceof",
			"!a instanceof b",
			rule.Given[NoUnsafeNegationOptions]{},
			"!(a instanceof b)",
			"(!a) instanceof b",
		},
		{
			"instanceof in parentheses",
			"(!a instanceof b)",
			rule.Given[NoUnsafeNegationOptions]{},
			"(!(a instanceof b))",
			"((!a) instanceof b)",
		},
		{
			"a parenthesised operand before instanceof",
			"!(a) instanceof b",
			rule.Given[NoUnsafeNegationOptions]{},
			"!((a) instanceof b)",
			"(!(a)) instanceof b",
		},
		{
			"less than, option on",
			"if (! a < b) {}",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"if (!(a < b)) {}",
			"if ((! a) < b) {}",
		},
		{
			"greater than, option on",
			"while (! a > b) {}",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"while (!(a > b)) {}",
			"while ((! a) > b) {}",
		},
		{
			"less or equal, option on",
			"foo = ! a <= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"foo = !(a <= b);",
			"foo = (! a) <= b;",
		},
		{
			"greater or equal, option on",
			"foo = ! a >= b;",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"foo = !(a >= b);",
			"foo = (! a) >= b;",
		},
		{
			"less or equal bare, option on",
			"!a <= b",
			rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}),
			"!(a <= b)",
			"(!a) <= b",
		},
		// A regex operand, which upstream's fix column skips: it is a fail-only case there, so this
		// expectation is ours and had to be derived rather than copied. Slicing the operand's
		// source text is the only thing that gets `/s/` back; anything reconstructing it from the
		// node would not.
		//
		// The single space before `(l)` is the honest output and was the fixture I got wrong first.
		// The first suggestion rebuilds the comparison as `operand operator right`, so it
		// normalises the author's `in(l)` to `in (l)`, exactly as upstream's `format!` does. That is
		// a whitespace change inside an expression the suggestion is already rewriting, and pinning
		// it here is better than pretending the rebuild preserves spacing it cannot see.
		{"a regex operand", "(y=>{if(!/s/ in(l)){}})", rule.Given[NoUnsafeNegationOptions]{},
			"(y=>{if(!(/s/ in (l))){}})", "(y=>{if((!/s/) in(l)){}})"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile,
				testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one diagnostic, got %d", len(result.Diagnostics))
			}
			if len(result.Diagnostics[0].Fixes) != 0 {
				test.Fatalf("wanted no unattended fixes, got %d", len(result.Diagnostics[0].Fixes))
			}
			suggestions := result.Diagnostics[0].Suggestions
			if len(suggestions) != 2 {
				test.Fatalf("wanted two suggestions, got %d", len(suggestions))
			}

			for index, want := range []string{testCase.wantNegated, testCase.wantExplicit} {
				if len(suggestions[index].Fixes) != 1 {
					test.Fatalf("suggestion %d carries %d fixes, wanted one", index, len(suggestions[index].Fixes))
				}
				fix := suggestions[index].Fixes[0]
				rewritten := testCase.sourceText[:fix.Range.Pos()] + fix.Text +
					testCase.sourceText[fix.Range.End():]
				if rewritten != want {
					test.Fatalf("suggestion %d rewrites to %q, wanted %q", index, rewritten, want)
				}
			}
		})
	}
}

// The two suggestions must not be the same edit, and they must be labelled differently.
//
// The whole reason this rule offers two is that the author's intent is unrecoverable from the
// source: one rewrite changes what the code does and the other pins what it already does. A port
// that emitted the same message for both, or that built the second from the first, would leave a
// human choosing between two identical-looking options.
func TestNoUnsafeNegationLabelsItsSuggestionsDistinctly(test *testing.T) {
	test.Parallel()

	result := rule_testing.Run(test, NoUnsafeNegation, unsafeNegationFile, "!a in b")
	if len(result.Diagnostics) != 1 || len(result.Diagnostics[0].Suggestions) != 2 {
		test.Fatalf("wanted one diagnostic carrying two suggestions")
	}
	first := result.Diagnostics[0].Suggestions[0].Message
	second := result.Diagnostics[0].Suggestions[1].Message
	if first.Id == second.Id {
		test.Fatalf("both suggestions carry the message id %q", first.Id)
	}
	if first.Description == second.Description {
		test.Fatalf("both suggestions carry the description %q", first.Description)
	}
}

// The message names the operator it saw, so a reader is told which one binds loosely than `!`.
//
// Two operators rather than one, because a rule quoting a constant string passes a single-operator
// assertion.
func TestNoUnsafeNegationNamesTheOperator(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    rule.Given[NoUnsafeNegationOptions]
		want       string
	}{
		{"!a in b", rule.Given[NoUnsafeNegationOptions]{}, "in"},
		{"!a instanceof b", rule.Given[NoUnsafeNegationOptions]{}, "instanceof"},
		{"!a <= b", rule.Configured(NoUnsafeNegationOptions{EnforceForOrderingRelations: true}), "<="},
	}

	for _, testCase := range cases {
		test.Run(testCase.want, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoUnsafeNegation, unsafeNegationFile,
				testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one diagnostic, got %d", len(result.Diagnostics))
			}
			description := result.Diagnostics[0].Message.Description
			if !strings.Contains(description, "`"+testCase.want+"`") {
				test.Fatalf("the description %q does not name the operator %q", description, testCase.want)
			}
		})
	}
}
