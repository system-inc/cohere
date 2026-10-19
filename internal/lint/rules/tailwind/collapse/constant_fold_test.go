package tailwind

import (
	"math"
	"testing"
)

// constantFoldCase is one input and the string upstream's constantFoldDeclaration returns for it.
type constantFoldCase struct {
	input    string
	expected string
}

// runConstantFoldCases checks each case through constantFoldDeclaration with upstream's defaults: no
// root font size, units normalized.
func runConstantFoldCases(t *testing.T, cases []constantFoldCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			if actual := constantFoldDeclaration(testCase.input, nil); actual != testCase.expected {
				t.Errorf("constantFoldDeclaration(%q) = %q, upstream gives %q", testCase.input, actual, testCase.expected)
			}
		})
	}
}

// TestConstantFoldExpressions ports upstream's "should constant fold %s into %s" table.
func TestConstantFoldExpressions(t *testing.T) {
	t.Parallel()
	runConstantFoldCases(t, []constantFoldCase{
		// Simple expression
		{"calc(1 + 1)", "2"},
		{"calc(3 - 2)", "1"},
		{"calc(2 * 3)", "6"},
		{"calc(8 / 2)", "4"},

		// Nested
		{"calc(1 + calc(1 + 1))", "3"},
		{"calc(3 - calc(1 + 2))", "0"},
		{"calc(2 * calc(1 + 3))", "8"},
		{"calc(8 / calc(2 + 2))", "2"},
		{"calc(1 + (1 + 1))", "3"},
		{"calc(3 - (1 + 2))", "0"},
		{"calc(2 * (1 + 3))", "8"},
		{"calc(8 / (2 + 2))", "2"},

		// With units
		{"calc(1rem * 2)", "2rem"},
		{"calc(2rem - 0.5rem)", "1.5rem"},
		{"calc(3rem * 6)", "18rem"},
		{"calc(5rem / 2)", "2.5rem"},

		// Negating with units
		{"calc(2rem * -1)", "-2rem"},
		{"calc(-1 * 2rem)", "-2rem"},

		// Nested partial evaluation
		{"calc(calc(1 + 2) + 2rem)", "calc(3 + 2rem)"},

		// Nested multiplication with unknown values
		{"calc(2 * calc(3 * var(--foo)))", "calc(6 * var(--foo))"},
		{"calc(calc(3 * var(--foo)) * 2)", "calc(6 * var(--foo))"},
		{"calc(2rem * calc(3 * var(--foo)))", "calc(6rem * var(--foo))"},

		// Nested addition with unknown values
		{"calc(1rem + calc(2rem + var(--foo)))", "calc(3rem + var(--foo))"},
		{"calc(calc(2rem + var(--foo)) + 1rem)", "calc(3rem + var(--foo))"},

		// Nested multiplication can collapse away entirely
		{"calc(-1 * calc(-1 * var(--foo)))", "var(--foo)"},
		{"calc(calc(-1 * var(--foo)) * -1)", "var(--foo)"},
		{"calc(-1 * calc(var(--foo) * -1))", "var(--foo)"},
		{"calc(-1 * (-1 * var(--foo)))", "var(--foo)"},
		{"calc(1 * var(--foo))", "var(--foo)"},
		{"calc(var(--foo) * 1)", "var(--foo)"},

		// Evaluation only handles two operands right now, this can change in the future
		{"calc(1 + 2 + 3)", "calc(1 + 2 + 3)"},
	})
}

// TestConstantFoldKeepsDifferentUnits ports upstream's "should not constant fold different units"
// table: each input comes back exactly as written.
func TestConstantFoldKeepsDifferentUnits(t *testing.T) {
	t.Parallel()
	var cases []constantFoldCase
	for _, input := range []string{
		"calc(1rem * 2%)",
		"calc(1rem * 2px)",
		"calc(2rem - 6)",
		"calc(3rem * 3dvw)",
		"calc(3rem * 2dvh)",
		"calc(5rem / 17px)",
		"calc(2rem * calc(3px * var(--foo)))",
		"calc(1rem + 0px + var(--foo))",
	} {
		cases = append(cases, constantFoldCase{input, input})
	}
	runConstantFoldCases(t, cases)
}

// TestConstantFoldToZero ports upstream's "should constant fold %s to 0" table.
func TestConstantFoldToZero(t *testing.T) {
	t.Parallel()
	var cases []constantFoldCase
	for _, input := range []string{
		"calc(0 * 100vw)",
		"calc(0 * calc(1 * 2))",
		"calc(0 * var(--foo))",
		"calc(0 * calc(var(--spacing) * 32))",

		"calc(100vw * 0)",
		"calc(calc(1 * 2) * 0)",
		"calc(var(--foo) * 0)",
		"calc(calc(var(--spacing, 0.25rem) * 32) * 0)",
		"calc(var(--spacing, 0.25rem) * -0)",

		// Zeroes
		"0px",
		"0rem",
		"0em",
		"0dvh",
		"-0",
		"+0",
		"-0.0rem",
		"+0.00rem",

		// Expressions
		"calc(-0px * -1)",
		"calc(-1 * -0px)",
	} {
		cases = append(cases, constantFoldCase{input, "0"})
	}
	runConstantFoldCases(t, cases)
}

// TestConstantFoldKeepsNonLengthZeroUnits ports upstream's "should not fold non-foldable units to 0"
// table.
func TestConstantFoldKeepsNonLengthZeroUnits(t *testing.T) {
	t.Parallel()
	runConstantFoldCases(t, []constantFoldCase{
		// Expressions, keep unit when they are nested
		{"calc(calc(0px * -1) + 1rem)", "calc(0px + 1rem)"},
		{"calc(calc(-1 * 0px) + 1rem)", "calc(0px + 1rem)"},

		// Non-foldable units
		{"0deg", "0deg"},
		{"0rad", "0deg"},
		{"0%", "0%"},
		{"0turn", "0deg"},
		{"0fr", "0fr"},
		{"0ms", "0s"},
		{"0s", "0s"},
		{"-0.0deg", "0deg"},
		{"-0.0rad", "0deg"},
		{"-0.0%", "0%"},
		{"-0.0turn", "0deg"},
		{"-0.0fr", "0fr"},
		{"-0.0ms", "0s"},
		{"-0.0s", "0s"},
	})
}

// TestConstantFoldDivision ports upstream's three single division cases.
func TestConstantFoldDivision(t *testing.T) {
	t.Parallel()
	runConstantFoldCases(t, []constantFoldCase{
		// should not constant fold when dividing by `0`
		{"calc(123rem / 0)", "calc(123rem / 0)"},
		// should not constant fold when a computation has a high-precision result
		{"calc(100% / 3.5)", "calc(100% / 3.5)"},
		// should constant fold division results with floating-point error after scaling
		{"calc(29% / 100)", "0.29%"},
	})
}

// TestConstantFoldJavaScriptNumberString holds javaScriptNumberString to `String(x)`, each expected
// string printed by Node 24. Not an upstream test: it pins the exponent thresholds and the signs the
// upstream tables never reach, since every folded number is printed through it.
func TestConstantFoldJavaScriptNumberString(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		value    float64
		expected string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{1, "1"},
		{-2.5, "-2.5"},
		{0.30000000000000004, "0.30000000000000004"},
		{1e21, "1e+21"},
		{999999999999999900000, "999999999999999900000"},
		{1.5e21, "1.5e+21"},
		{-1e21, "-1e+21"},
		{0.000001, "0.000001"},
		{0.0000001, "1e-7"},
		{1.25e-7, "1.25e-7"},
		{123456789012, "123456789012"},
		{3779.527559055118, "3779.527559055118"},
		{math.Inf(1), "Infinity"},
		{math.Inf(-1), "-Infinity"},
		{math.NaN(), "NaN"},
		{5e-324, "5e-324"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
	} {
		if actual := javaScriptNumberString(testCase.value); actual != testCase.expected {
			t.Errorf("javaScriptNumberString(%v) = %q, JavaScript prints %q", testCase.value, actual, testCase.expected)
		}
	}
}
