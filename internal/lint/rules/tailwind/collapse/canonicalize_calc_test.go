package tailwind

import "testing"

// TestCanonicalizeCalcExpressions ports upstream's two tables: the expressions whose operands are
// reordered, and the ones kept exactly as written.
func TestCanonicalizeCalcExpressions(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		input    string
		expected string
	}{
		{"calc(-1 * var(--foo))", "calc(var(--foo) * -1)"},
		{"calc(1rem + var(--foo))", "calc(var(--foo) + 1rem)"},
		{"calc(2rem * calc(3px * var(--foo)))", "calc(calc(var(--foo) * 3px) * 2rem)"},
		{"calc(var(--b) + var(--a))", "calc(var(--a) + var(--b))"},
		{"calc(3px * 2rem)", "calc(2rem * 3px)"},
		{"calc(5px * 3px)", "calc(3px * 5px)"},
		{"calc(1 * 1rem)", "calc(1rem * 1)"},
		{"calc(10 + 2)", "calc(2 + 10)"},

		// should keep
		{"calc(1rem - var(--foo))", "calc(1rem - var(--foo))"},
		{"calc(1rem / 2)", "calc(1rem / 2)"},
		{"calc(var(--a) + 1rem)", "calc(var(--a) + 1rem)"},
		{"calc(2rem * 3px)", "calc(2rem * 3px)"},
	} {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()
			if actual := canonicalizeCalcExpressions(testCase.input); actual != testCase.expected {
				t.Errorf("canonicalizeCalcExpressions(%q) = %q, upstream gives %q", testCase.input, actual, testCase.expected)
			}
		})
	}
}

// TestCanonicalizeCalcLocaleCompare holds javaScriptLocaleCompare to the sign of `localeCompare` in
// Node 24, locale `en-US`, on the pairs where collation and code point order disagree. Not an
// upstream test: upstream's tables only compare strings that sort the same either way.
func TestCanonicalizeCalcLocaleCompare(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		left     string
		right    string
		expected int
	}{
		{"var(--a)", "var(--b)", -1},
		{"rem", "px", 1},
		{"a", "A", -1},
		{"aB", "Ab", -1},
		{"Ab", "aC", -1},
		{"B", "a", 1},
		{"a-b", "a_b", 1},
		{"A", "a-", -1},
		{"$", "0", -1},
		{"~", "$", -1},
		{"1", "1.0", -1},
		{"a\u0001b", "ab", 0},
		{"a\tb", "a b", -1},
		{"%", "px", -1},
		{"VAR(--a)", "var(--a)", 1},
	} {
		order, collated := javaScriptLocaleCompare(testCase.left, testCase.right)
		if !collated || order != testCase.expected {
			t.Errorf("javaScriptLocaleCompare(%q, %q) = %d, %v, Node gives %d", testCase.left, testCase.right, order, collated, testCase.expected)
		}
	}

	if _, collated := javaScriptLocaleCompare("var(--é)", "var(--a)"); collated {
		t.Errorf("javaScriptLocaleCompare collated a string outside ASCII, which it has no tables for")
	}
}
