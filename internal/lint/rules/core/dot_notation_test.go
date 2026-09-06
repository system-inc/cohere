package core

import (
	"testing"

	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// dotNotationFile is where the fixtures pretend to live.
//
// A `.ts` name: this rule has no file gate, and nothing in its corpus is JSX.
const dotNotationFile = "/repository/source/DotNotation.ts"

// dotNotationCase is one row of upstream's corpus.
type dotNotationCase struct {
	// name is the corpus list and index the row came from, so a failure names a case that
	// can be found in upstream's own file rather than a number local to this table.
	name string

	// source is upstream's `code`, byte for byte.
	source string

	// options is the RAW JSON of upstream's options object, routed through the rule's own
	// exported decoder rather than built as a struct, so the decoder's defaults and its
	// empty-input path are under test.
	options string

	// ids are the message ids upstream produced for this input, in order.
	ids []string
}

// dotNotationFiresCases are the rows upstream reports on.
var dotNotationFiresCases = []dotNotationCase{
	{
		name:    "invalid-0",
		source:  "a.true;",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
	{
		name:    "invalid-1",
		source:  "a['true'];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-2",
		source:  "a[`time`];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-3",
		source:  "a[null];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-4",
		source:  "a[true];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-5",
		source:  "a[false];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-6",
		source:  "a['b'];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-7",
		source:  "a.b['c'];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-8",
		source:  "a['_dangle'];",
		options: "{\"allowPattern\": \"^[a-z]+(_[a-z]+)+$\"}",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-9",
		source:  "a['SHOUT_CASE'];",
		options: "{\"allowPattern\": \"^[a-z]+(_[a-z]+)+$\"}",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-10",
		source:  "a\n  ['SHOUT_CASE'];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-11",
		source:  "getResource()\n    .then(function(){})\n    [\"catch\"](function(){})\n    .then(function(){})\n    [\"catch\"](function(){});",
		options: "",
		ids:     []string{"useDot", "useDot"},
	},
	{
		name:    "invalid-12",
		source:  "foo\n  .while;",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
	{
		name:    "invalid-13",
		source:  "foo[ /* comment */ 'bar' ]",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-14",
		source:  "foo[ 'bar' /* comment */ ]",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-15",
		source:  "foo[    'bar'    ];",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-16",
		source:  "foo. /* comment */ while",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
	{
		name:    "invalid-17",
		source:  "foo[('bar')]",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-18",
		source:  "foo[(null)]",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-19",
		source:  "(foo)['bar']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-20",
		source:  "1['toString']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-21",
		source:  "foo['bar']instanceof baz",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-22",
		source:  "let.if()",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
	{
		name:    "invalid-23",
		source:  "5['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-24",
		source:  "-5['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-25",
		source:  "01['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-26",
		source:  "01234567['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-27",
		source:  "08['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-28",
		source:  "090['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-29",
		source:  "018['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-30",
		source:  "5_000['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-31",
		source:  "5_000_00['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-32",
		source:  "5.000_000['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-33",
		source:  "0b1010_1010['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-34",
		source:  "obj?.['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-35",
		source:  "0?.['prop']",
		options: "",
		ids:     []string{"useDot"},
	},
	{
		name:    "invalid-36",
		source:  "obj?.true",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
	{
		name:    "invalid-37",
		source:  "let?.true",
		options: "{\"allowKeywords\": false}",
		ids:     []string{"useBrackets"},
	},
}

// dotNotationSilentCases are the rows upstream is clean on.
var dotNotationSilentCases = []dotNotationCase{
	{
		name:    "valid-0",
		source:  "a.b;",
		options: "",
	},
	{
		name:    "valid-1",
		source:  "a.b.c;",
		options: "",
	},
	{
		name:    "valid-2",
		source:  "a['12'];",
		options: "",
	},
	{
		name:    "valid-3",
		source:  "a[b];",
		options: "",
	},
	{
		name:    "valid-4",
		source:  "a[0];",
		options: "",
	},
	{
		name:    "valid-5",
		source:  "a.b.c;",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-6",
		source:  "a.arguments;",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-7",
		source:  "a.let;",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-8",
		source:  "a.yield;",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-9",
		source:  "a.eval;",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-10",
		source:  "a[0];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-11",
		source:  "a['while'];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-12",
		source:  "a['true'];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-13",
		source:  "a['null'];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-14",
		source:  "a[true];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-15",
		source:  "a[null];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-16",
		source:  "a.true;",
		options: "{\"allowKeywords\": true}",
	},
	{
		name:    "valid-17",
		source:  "a.null;",
		options: "{\"allowKeywords\": true}",
	},
	{
		name:    "valid-18",
		source:  "a['snake_case'];",
		options: "{\"allowPattern\": \"^[a-z]+(_[a-z]+)+$\"}",
	},
	{
		name:    "valid-19",
		source:  "a['lots_of_snake_case'];",
		options: "{\"allowPattern\": \"^[a-z]+(_[a-z]+)+$\"}",
	},
	{
		name:    "valid-20",
		source:  "a[`time${range}`];",
		options: "",
	},
	{
		name:    "valid-21",
		source:  "a[`while`];",
		options: "{\"allowKeywords\": false}",
	},
	{
		name:    "valid-22",
		source:  "a[`time range`];",
		options: "",
	},
	{
		name:    "valid-23",
		source:  "a.true;",
		options: "",
	},
	{
		name:    "valid-24",
		source:  "a.null;",
		options: "",
	},
	{
		name:    "valid-25",
		source:  "a[undefined];",
		options: "",
	},
	{
		name:    "valid-26",
		source:  "a[void 0];",
		options: "",
	},
	{
		name:    "valid-27",
		source:  "a[b()];",
		options: "",
	},
	{
		name:    "valid-28",
		source:  "a[/(?<zero>0)/];",
		options: "",
	},
	{
		name:    "valid-29",
		source:  "class C { foo() { this['#a'] } }",
		options: "",
	},
	{
		name:    "valid-30",
		source:  "class C { #in; foo() { this.#in; } }",
		options: "{\"allowKeywords\": false}",
	},
}

// decodedDotNotation routes a row's raw JSON through the rule's own exported decoder.
func decodedDotNotation(t *testing.T, raw string) any {
	t.Helper()
	decoded, err := DecodeDotNotationOptions([]byte(raw))
	if err != nil {
		t.Fatalf("decoding options %q: %v", raw, err)
	}
	return decoded
}

func TestDotNotationFires(t *testing.T) {
	t.Parallel()
	for _, testCase := range dotNotationFiresCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, DotNotation, dotNotationFile, testCase.source,
				decodedDotNotation(t, testCase.options))
			rule_testing.ExpectFindings(t, result, testCase.ids...)
		})
	}
}

func TestDotNotationStaysSilent(t *testing.T) {
	t.Parallel()
	for _, testCase := range dotNotationSilentCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunWithOptions(t, DotNotation, dotNotationFile, testCase.source,
				decodedDotNotation(t, testCase.options))
			rule_testing.ExpectClean(t, result)
		})
	}
}
