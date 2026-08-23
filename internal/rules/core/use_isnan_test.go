package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const isNaNFile = "/repository/source/Thing.ts"

func TestUseIsNaNFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"strict equality", "declare const value: number;\nexport const Bad = value === NaN;\n"},
		{"loose equality", "declare const value: number;\nexport const Bad = value == NaN;\n"},
		{"strict inequality", "declare const value: number;\nexport const Bad = value !== NaN;\n"},
		// NaN on the left rather than the right. A check reading only one operand misses half.
		{"NaN on the left", "declare const value: number;\nexport const Bad = NaN === value;\n"},
		// Relational operators are always false too, which is why they are in the operator set.
		{"less than", "declare const value: number;\nexport const Bad = value < NaN;\n"},
		{"greater or equal", "declare const value: number;\nexport const Bad = value >= NaN;\n"},
		// The same value reached through the constructor.
		{"Number.NaN", "declare const value: number;\nexport const Bad = value === Number.NaN;\n"},
		{"parenthesized", "declare const value: number;\nexport const Bad = value === (NaN);\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, UseIsNaN, isNaNFile, testCase.sourceText),
				"comparisonWithNaN")
		})
	}
}

func TestUseIsNaNStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"the correct check", "declare const value: number;\nexport const Ok = Number.isNaN(value);\n"},
		// The decision boundary is "does an operand name NaN", and the way to get it wrong is to
		// match the letters. Each of these contains them and none is the value.
		{"a similarly named binding", "declare const isNaNLike: boolean;\nexport const Ok = isNaNLike === true;\n"},
		{"a property named NaN on something else", "declare const table: { NaN: number };\ndeclare const value: number;\nexport const Ok = value === table.NaN;\n"},
		{"the letters in a string", "declare const value: string;\nexport const Ok = value === 'NaN';\n"},
		// Not a comparison. Arithmetic with NaN is legal and produces NaN, which is not this
		// rule's business; only comparisons are always-constant.
		{"arithmetic rather than comparison", "declare const value: number;\nexport const Ok = value + NaN;\n"},
		{"a plain comparison", "declare const value: number;\nexport const Ok = value === 0;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, UseIsNaN, isNaNFile, testCase.sourceText))
		})
	}
}
