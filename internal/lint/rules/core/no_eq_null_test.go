package core

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// eqNullFile is where the fixtures pretend to live.
const eqNullFile = "/repository/source/EqNull.ts"

// The corpus is ESLint's own, at `tests/lib/rules/no-eq-null.js`, copied rather than rewritten.
//
// It is small: two valid cases and three invalid ones, and that is the whole upstream file. Each
// invalid case carries its own line and column assertions, which is why the span test below exists
// rather than being optional.
func TestNoEqNullFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"loose equality with null on the right", "if (x == null) { }"},
		{"loose inequality with null on the right", "if (x != null) { }"},
		{"loose equality with null on the left", "do {} while (null == x)"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test,
				rule_testing.Run(test, NoEqNull, eqNullFile, testCase.sourceText), "unexpected")
		})
	}
}

// Both upstream clean cases use `===`, which is the whole point of the rule.
func TestNoEqNullStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"strict equality with null", "if (x === null) { }"},
		{"strict equality with null on the left", "if (null === f()) { }"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.Run(test, NoEqNull, eqNullFile, testCase.sourceText))
		})
	}
}

// Cases upstream does not write, each measured against ESLint 10.8.1 with `sourceType: "script"`
// before being recorded here rather than reasoned about.
//
// The parenthesized pair is the important one and it runs the opposite way to how the upstream
// source reads. `node.right.type === "Literal"` looks like it must decline a parenthesized operand,
// and it does not, because espree produces no node for parentheses. typescript-go does, so this
// pair is what pins the `SkipParentheses` call in the rule as fidelity rather than a widening.
func TestNoEqNullFiresOnCasesBeyondTheCorpus(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"a parenthesized null on the right", "if (x == (null)) { }"},
		{"a parenthesized null on the left", "if ((null) == x) { }"},
		{"null on both sides, reported once", "if (null == null) { }"},
		{"outside a condition", "x == null ? 1 : 2"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test,
				rule_testing.Run(test, NoEqNull, eqNullFile, testCase.sourceText), "unexpected")
		})
	}
}

// The clean cases beyond the corpus are what pin the predicate as being about the literal's
// spelling rather than about the value compared, which is the part of upstream that reads like an
// oversight and is not. All four measured clean against the installed build.
func TestNoEqNullStaysSilentOnCasesBeyondTheCorpus(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// The same comparison `x == null` performs, spelled differently, and upstream is silent.
		{"loose equality with undefined", "if (x == undefined) { }"},
		// A variable holding null is not the literal, so no spelling rule can see it.
		{"loose equality with a variable holding null", "let n = null; if (x == n) {}"},
		// A string whose text is `null` is a different value entirely.
		{"loose equality with the string null", "if (x == \"null\") {}"},
		// Not a comparison operator at all.
		{"a relational operator against null", "if (x < null) { }"},
		// Strict inequality is the other half of the repair the rule asks for.
		{"strict inequality with null", "if (x !== null) { }"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.Run(test, NoEqNull, eqNullFile, testCase.sourceText))
		})
	}
}

// Every invalid case in upstream's corpus carries line and column assertions, so the span is stated
// data rather than something recovered. These are those three cases, with the upstream columns
// converted to the slice the finding names.
//
// `if (x == null) { }` asserts columns 5 through 14, one-based and end-exclusive, which is the
// zero-based half-open range [4,13) and the text `x == null`. The parenthesized case is included
// because upstream's columns 5 through 16 say the parentheses are inside the reported span even
// though the null they wrap is what triggered the finding.
func TestNoEqNullSpansTheWholeComparison(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name         string
		sourceText   string
		wantReported string
	}{
		{"null on the right", "if (x == null) { }", "x == null"},
		{"loose inequality", "if (x != null) { }", "x != null"},
		{"null on the left", "do {} while (null == x)", "null == x"},
		{"parentheses are inside the span", "if (x == (null)) { }", "x == (null)"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoEqNull, eqNullFile, testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("want one finding, got %d", len(result.Diagnostics))
			}
			reported := testCase.sourceText[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
			if reported != testCase.wantReported {
				test.Errorf("the finding points at %q, want %q", reported, testCase.wantReported)
			}
		})
	}
}

// The message text is asserted against a literal typed here rather than against the rule's own
// constant, because comparing a diagnostic to the constant it was built from moves both sides
// together under mutation and asserts nothing.
func TestNoEqNullMessage(test *testing.T) {
	test.Parallel()

	result := rule_testing.Run(test, NoEqNull, eqNullFile, "if (x == null) { }")
	if len(result.Diagnostics) != 1 {
		test.Fatalf("want one finding, got %d", len(result.Diagnostics))
	}
	if got := result.Diagnostics[0].Message.Id; got != "unexpected" {
		test.Errorf("message id is %q, want %q", got, "unexpected")
	}
	if got := result.Diagnostics[0].Message.Description; !strings.HasPrefix(
		got,
		"This compares against `null` with `==` or `!=`",
	) {
		test.Errorf("message description starts %q, which is not the sentence this rule reports", got)
	}
}
