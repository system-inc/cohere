package estree

import (
	"math"
	"testing"
)

// IsTruthy is read at every `if (node.key)` the printers port, about 160 places, and the fork tests reach only
// the variants their corpora happen to hold: a mutant that made `0`, NaN, a nil child or an empty list truthy
// the wrong way passed them all. So each variant's JavaScript truthiness is pinned here, on its own.
func TestIsTruthyAnswersAsJavaScriptDoesForEveryVariant(test *testing.T) {
	test.Parallel()

	var nilNode *Node
	cases := []struct {
		name   string
		value  Value
		truthy bool
	}{
		{"an absent property", nil, false},
		{"false", Boolean(false), false},
		{"true", Boolean(true), true},
		{"the empty string", String(""), false},
		{"a string", String("a"), true},
		{"the number zero", Number(0), false},
		{"negative zero", Number(math.Copysign(0, -1)), false},
		{"NaN", Number(math.NaN()), false},
		{"a fraction", Number(0.5), true},
		{"the integer zero", Integer(0), false},
		{"a negative integer", Integer(-1), true},
		{"a nil child", nilNode, false},
		{"a child", New("Identifier", 0, 1, nil), true},
		{"a nil array of children", Nodes(nil), true},
		{"an empty array of children", Nodes{}, true},
		{"a nil mixed array", Values(nil), true},
		{"an empty mixed array", Values{}, true},
		{"a nil object", Record(nil), true},
		{"an empty object", Record{}, true},
		{"a template value", &TemplateValue{Raw: ""}, true},
		{"a regular expression", &Regex{Pattern: "a"}, true},
	}
	for _, each := range cases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()

			if got := IsTruthy(each.value); got != each.truthy {
				test.Errorf("IsTruthy(%#v) is %v, and JavaScript's truthiness is %v", each.value, got, each.truthy)
			}
		})
	}
}
