package css

import (
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
)

// isPresent and firstTruthyString decide what the printer prints from the glue's typed values, and the fork
// tests' corpora never reach their mixed-list or empty-string branches: mutants there passed every fork test.
// Each branch is pinned here on its own.

func TestIsPresentTellsAnAbsentListFromAnEmptyOne(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name    string
		value   estree.Value
		present bool
	}{
		{"an absent array of children", estree.Nodes(nil), false},
		{"an empty array of children", estree.Nodes{}, true},
		{"an absent mixed array", estree.Values(nil), false},
		{"an empty mixed array", estree.Values{}, true},
		{"an absent property", nil, false},
		{"the empty string", estree.String(""), false},
		{"a string", estree.String("a"), true},
		{"the integer zero", estree.Integer(0), false},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			if got := isPresent(each.value); got != each.present {
				test.Errorf("isPresent(%#v) is %v, want %v", each.value, got, each.present)
			}
		})
	}
}

func TestFirstTruthyStringSkipsWhatJavaScriptsOrSkips(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		values []estree.Value
		want   string
	}{
		{"the first of two strings", []estree.Value{estree.String("a"), estree.String("b")}, "a"},
		{"past an absent value", []estree.Value{nil, estree.String("b")}, "b"},
		{"past the empty string", []estree.Value{estree.String(""), estree.String("b")}, "b"},
		{"nothing truthy", []estree.Value{nil, estree.String("")}, ""},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			if got := firstTruthyString(each.values...); got != each.want {
				test.Errorf("firstTruthyString(%#v) is %q, want %q", each.values, got, each.want)
			}
		})
	}
}
