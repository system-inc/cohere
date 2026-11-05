package tailwind

import (
	"reflect"
	"testing"
)

// The first three tests are upstream's `attribute-selector-parser.test.ts` at v4.3.3, case for case.
// Upstream's `toBeNull()` is a false second result here, and its `toEqual` with a null value is a
// nil Value.
//
// The last test is not upstream's. It pins the branches upstream's own tests leave unexercised:
// every operator, the uppercase flags, escapes, and the three JavaScript-shaped behaviors described
// at the top of attribute_selector_parser.go. Its expectations were measured by running the v4.3.3
// source under Node rather than written by reading it, because those behaviors are the ones a
// careful reader gets wrong in the direction of a stricter parser.

// attributeSelectorValue is a present Value, spelled inline in a table.
func attributeSelectorValue(value string) *string {
	present := value
	return &present
}

func TestParseAttributeSelectorRejectsInvalid(test *testing.T) {
	test.Parallel()
	for _, input := range []string{
		``,
		`]`,
		`[]`,
		`[`,
		`="value"`,
		`data-foo]`,
		`[data-foo`,
		`[data-foo="foo]`,
		`[data-foo * = foo]`,
		`[data-foo*=]`,
		`[data-foo=value x]`,
		`[data-foo=value ix]`,
	} {
		test.Run(input, func(test *testing.T) {
			test.Parallel()
			if selector, ok := ParseAttributeSelector(input); ok {
				test.Errorf(
					"ParseAttributeSelector(%q) = %s, want rejected",
					input,
					describeAttributeSelector(selector),
				)
			}
		})
	}
}

func TestParseAttributeSelectorParsesValid(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		input    string
		expected AttributeSelector
	}{
		{`[data-foo]`, AttributeSelector{Attribute: "data-foo"}},
		{`[ data-foo ]`, AttributeSelector{Attribute: "data-foo"}},
		// Non-ASCII code points are valid in a CSS ident.
		{`[data-état]`, AttributeSelector{Attribute: "data-état"}},
		{
			`[data-state=expanded]`,
			AttributeSelector{Attribute: "data-state", Operator: "=", Value: attributeSelectorValue("expanded")},
		},
		{
			`[data-state = expanded ]`,
			AttributeSelector{Attribute: "data-state", Operator: "=", Value: attributeSelectorValue("expanded")},
		},
		{`[data-state*="expanded"]`, AttributeSelector{
			Attribute: "data-state",
			Operator:  "*=",
			Quote:     `"`,
			Value:     attributeSelectorValue("expanded"),
		}},
		{`[data-state*="expanded"i]`, AttributeSelector{
			Attribute:   "data-state",
			Operator:    "*=",
			Quote:       `"`,
			Value:       attributeSelectorValue("expanded"),
			Sensitivity: "i",
		}},
		{`[data-state*=expanded i]`, AttributeSelector{
			Attribute:   "data-state",
			Operator:    "*=",
			Value:       attributeSelectorValue("expanded"),
			Sensitivity: "i",
		}},
	} {
		test.Run(testCase.input, func(test *testing.T) {
			test.Parallel()
			assertAttributeSelector(test, testCase.input, testCase.expected)
		})
	}
}

func TestParseAttributeSelectorRealWorldExample(test *testing.T) {
	test.Parallel()
	assertAttributeSelector(test, `[data-url$=".com"i]`, AttributeSelector{
		Attribute:   "data-url",
		Operator:    "$=",
		Quote:       `"`,
		Value:       attributeSelectorValue(".com"),
		Sensitivity: "i",
	})
}

func TestParseAttributeSelectorMatchesUpstreamBeyondItsTests(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name     string
		input    string
		expected *AttributeSelector
	}{
		{
			"operator includes",
			`[a~=b]`,
			&AttributeSelector{Attribute: "a", Operator: "~=", Value: attributeSelectorValue("b")},
		},
		{
			"operator dash match",
			`[a|=b]`,
			&AttributeSelector{Attribute: "a", Operator: "|=", Value: attributeSelectorValue("b")},
		},
		{
			"operator prefix",
			`[a^=b]`,
			&AttributeSelector{Attribute: "a", Operator: "^=", Value: attributeSelectorValue("b")},
		},
		{
			"operator suffix",
			`[a$=b]`,
			&AttributeSelector{Attribute: "a", Operator: "$=", Value: attributeSelectorValue("b")},
		},
		{"unknown operator", `[a!=b]`, nil},
		{"whitespace only", `[ ]`, nil},
		{"uppercase flag lowercased", `[a="v"S]`, &AttributeSelector{
			Attribute:   "a",
			Operator:    "=",
			Quote:       `"`,
			Value:       attributeSelectorValue("v"),
			Sensitivity: "s",
		}},
		{"single quote and s flag", `[a='v' s ]`, &AttributeSelector{
			Attribute:   "a",
			Operator:    "=",
			Quote:       `'`,
			Value:       attributeSelectorValue("v"),
			Sensitivity: "s",
		}},
		// An unquoted value runs to whitespace, so a flag written against it joins the value.
		{
			"flag against unquoted value",
			`[a=bi]`,
			&AttributeSelector{Attribute: "a", Operator: "=", Value: attributeSelectorValue("bi")},
		},
		// Tab, line feed and carriage return are whitespace. Form feed is not, so it stays in the value.
		{
			"ascii whitespace",
			"[a\t=\nb\r]",
			&AttributeSelector{Attribute: "a", Operator: "=", Value: attributeSelectorValue("b")},
		},
		{
			"form feed is not whitespace",
			"[a=b\fi]",
			&AttributeSelector{Attribute: "a", Operator: "=", Value: attributeSelectorValue("b\fi")},
		},
		{
			"escaped space in name",
			`[a\ b=c]`,
			&AttributeSelector{Attribute: `a\ b`, Operator: "=", Value: attributeSelectorValue("c")},
		},
		{"escaped astral character in name", `[a\😀]`, &AttributeSelector{Attribute: `a\😀`}},
		{
			"astral character in value",
			`[a=😀]`,
			&AttributeSelector{Attribute: "a", Operator: "=", Value: attributeSelectorValue("😀")},
		},
		{
			"escaped quote in value",
			`[a="x\"y"]`,
			&AttributeSelector{Attribute: "a", Operator: "=", Quote: `"`, Value: attributeSelectorValue(`x\"y`)},
		},
		// An escape at the end of the name steps over the `]`, and the read past the end rejects.
		{"escaped closing bracket", `[a\]`, nil},
		// The last matching quote closes the value, not the first.
		{
			"last quote closes",
			`[a="x"y"]`,
			&AttributeSelector{Attribute: "a", Operator: "=", Quote: `"`, Value: attributeSelectorValue(`x"y`)},
		},
		// An unterminated quote is an empty value, and what follows the quote is read as the flag.
		{"unterminated quote", `[a="i]`, &AttributeSelector{
			Attribute:   "a",
			Operator:    "=",
			Quote:       `"`,
			Value:       attributeSelectorValue(""),
			Sensitivity: "i",
		}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if testCase.expected == nil {
				if selector, ok := ParseAttributeSelector(testCase.input); ok {
					test.Errorf(
						"ParseAttributeSelector(%q) = %s, want rejected",
						testCase.input,
						describeAttributeSelector(selector),
					)
				}
				return
			}
			assertAttributeSelector(test, testCase.input, *testCase.expected)
		})
	}
}

// assertAttributeSelector parses input and compares every field, following Value's pointer, so a
// nil Value and a present empty one are different results, as upstream's null and empty string are.
func assertAttributeSelector(test *testing.T, input string, expected AttributeSelector) {
	test.Helper()
	selector, ok := ParseAttributeSelector(input)
	if !ok {
		test.Fatalf("ParseAttributeSelector(%q) rejected, want %s", input, describeAttributeSelector(expected))
	}
	if !reflect.DeepEqual(selector, expected) {
		test.Errorf(
			"ParseAttributeSelector(%q) = %s, want %s",
			input,
			describeAttributeSelector(selector),
			describeAttributeSelector(expected),
		)
	}
}

// describeAttributeSelector prints a selector with its Value dereferenced, since %+v would print
// the pointer's address.
func describeAttributeSelector(selector AttributeSelector) string {
	value := "<nil>"
	if selector.Value != nil {
		value = `"` + *selector.Value + `"`
	}
	return "{Attribute:" + selector.Attribute + " Operator:" + selector.Operator + " Quote:" + selector.Quote +
		" Value:" + value + " Sensitivity:" + selector.Sensitivity + "}"
}
