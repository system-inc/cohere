package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const typeofFile = "/repository/source/Thing.ts"

func TestValidTypeofFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"a misspelling", "declare const value: unknown;\nexport const Bad = typeof value === 'strng';\n"},
		// The two that are not typos but wrong beliefs about the language. An array reports as
		// 'object' and so does null, which is the oldest wart in JavaScript.
		{"array is not a typeof result", "declare const value: unknown;\nexport const Bad = typeof value === 'array';\n"},
		{"null is not a typeof result", "declare const value: unknown;\nexport const Bad = typeof value === 'null';\n"},
		{"typeof on the right", "declare const value: unknown;\nexport const Bad = 'array' === typeof value;\n"},
		{"inequality", "declare const value: unknown;\nexport const Bad = typeof value !== 'array';\n"},
		{"a template literal with no substitutions", "declare const value: unknown;\nexport const Bad = typeof value === `array`;\n"},
		{"parenthesized typeof", "declare const value: unknown;\nexport const Bad = (typeof value) === 'array';\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectFindings(t, rule_testing.Run(t, ValidTypeof, typeofFile, testCase.sourceText),
				"invalidTypeofValue")
		})
	}
}

func TestValidTypeofStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		// All eight legal results. These are the decision boundary: the rule's entire job is
		// knowing exactly this set, and a rule that dropped one would report correct code.
		{"undefined", "declare const value: unknown;\nexport const Ok = typeof value === 'undefined';\n"},
		{"object", "declare const value: unknown;\nexport const Ok = typeof value === 'object';\n"},
		{"boolean", "declare const value: unknown;\nexport const Ok = typeof value === 'boolean';\n"},
		{"number", "declare const value: unknown;\nexport const Ok = typeof value === 'number';\n"},
		{"string", "declare const value: unknown;\nexport const Ok = typeof value === 'string';\n"},
		{"function", "declare const value: unknown;\nexport const Ok = typeof value === 'function';\n"},
		{"symbol", "declare const value: unknown;\nexport const Ok = typeof value === 'symbol';\n"},
		{"bigint", "declare const value: unknown;\nexport const Ok = typeof value === 'bigint';\n"},

		// Unanalyzable without the checker, so left alone rather than guessed at.
		{"typeof compared to typeof", "declare const a: unknown;\ndeclare const b: unknown;\nexport const Ok = typeof a === typeof b;\n"},
		{"typeof compared to a binding", "declare const value: unknown;\ndeclare const expected: string;\nexport const Ok = typeof value === expected;\n"},
		// No typeof at all: a wrong-looking string on its own is not this rule's business.
		{"a plain string comparison", "declare const value: string;\nexport const Ok = value === 'array';\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rule_testing.ExpectClean(t, rule_testing.Run(t, ValidTypeof, typeofFile, testCase.sourceText))
		})
	}
}
