package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// sortVariablesFile is where the fixtures pretend to live.
const sortVariablesFile = "/repository/source/SortVars.ts"

// sortVariablesOf builds the settings a config carrying this option would decode to.
//
// The argument is the option as JSON text, exactly the bytes the config layer hands the decoder.
func sortVariablesOf(optionJson string) rule.Given[SortVariablesSettings] {
	settings, decodeSortVariablesOptionsError := DecodeSortVariablesOptions([]byte(optionJson))
	if decodeSortVariablesOptionsError != nil {
		panic(decodeSortVariablesOptionsError)
	}
	return rule.Configured(settings)
}

// pointerTo distinguishes "the corpus declared no output" from "the output is this string".
//
// Shared by every fixable rule's fixture table in this package, declared here because sort-vars
// was the first to need it. A Go package is one namespace, so a second declaration in another
// rule's test file is a build error rather than a local helper.
func pointerTo(value string) *string { return new(value) }

// The corpus is ESLint's own, taken from
// /tmp/lint-sources-fresh/eslint/tests/lib/rules/sort-vars.js by running that file with RuleTester
// intercepted. 61 cases from one RuleTester.run call.
//
// Every expectation is what the installed ESLint 10.8.1 rule answered, driven through the Linter
// interface under `sourceType: module` with the typescript-eslint parser. The oracle reproduced all
// 61 declared verdicts and all 25 declared outputs, which is the control saying the fixer half of
// the harness measures the fixer rather than nothing. Zero cases change verdict between upstream's
// configuration and cohere's.
//
// # The repair columns are the point of this table
//
// Section 3b of the standard is blunt about why: a fixer that reports in the right place and
// repairs wrongly is indistinguishable, at the message-id layer, from a correct one. So a corpus
// `output` is not optional coverage, it is the only coverage the fixer has.
//
// Upstream's corpus states three different things and they need three different assertions:
//
//	no output declared    the corpus pinned nothing, so only the findings are asserted
//	output: null          upstream REPORTS and deliberately declines to repair, so the
//	                      assertion is that this rule proposes no fix either
//	output: "<source>"    ExpectFixedSource compares the whole rewritten file
//
// Collapsing the middle case into "the output equals the input" does not work and the harness is
// right to refuse it: `ExpectFixedSource` fatals when a rule proposed no fixes, because passing
// there would make it agree with any expectation at all. A decline has to be asserted as an
// absence of fixes, not as an unchanged string.
//
//	25 reporting cases (9 of them declines), 36 clean cases.
func TestSortVariablesFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source   string
		settings rule.Given[SortVariablesSettings]
		findings int
		// fixed is the source after one fix pass, which is what upstream's `output` records.
		fixed *string
		// declinesRepair marks upstream's `output: null`: reported, deliberately not fixed.
		declinesRepair bool
	}{
		{source: "var b, a", findings: 1, fixed: pointerTo("var a, b"), declinesRepair: false},
		{source: "var b , a", findings: 1, fixed: pointerTo("var a , b"), declinesRepair: false},
		{
			source:         "var b,\n    a;",
			findings:       1,
			fixed:          pointerTo("var a,\n    b;"),
			declinesRepair: false,
		},
		{
			source:         "var b=10, a=20;",
			findings:       1,
			fixed:          pointerTo("var a=20, b=10;"),
			declinesRepair: false,
		},
		{
			source:         "var b=10, a=20, c=30;",
			findings:       1,
			fixed:          pointerTo("var a=20, b=10, c=30;"),
			declinesRepair: false,
		},
		{
			source:         "var all=10, a = 1",
			findings:       1,
			fixed:          pointerTo("var a = 1, all=10"),
			declinesRepair: false,
		},
		{
			source:         "var b, c, a, d",
			findings:       1,
			fixed:          pointerTo("var a, b, c, d"),
			declinesRepair: false,
		},
		{
			source:         "var c, d, a, b",
			findings:       2,
			fixed:          pointerTo("var a, b, c, d"),
			declinesRepair: false,
		},
		{source: "var a, A;", findings: 1, fixed: pointerTo("var A, a;"), declinesRepair: false},
		{source: "var a, B;", findings: 1, fixed: pointerTo("var B, a;"), declinesRepair: false},
		{source: "var a, B, c;", findings: 1, fixed: pointerTo("var B, a, c;"), declinesRepair: false},
		{
			source:         "var B, a;",
			settings:       sortVariablesOf("{\"ignoreCase\": true}"),
			findings:       1,
			fixed:          pointerTo("var a, B;"),
			declinesRepair: false,
		},
		{
			source:         "var B, A, c;",
			settings:       sortVariablesOf("{\"ignoreCase\": true}"),
			findings:       1,
			fixed:          pointerTo("var A, B, c;"),
			declinesRepair: false,
		},
		{
			source:         "var d, a, [b, c] = {};",
			settings:       sortVariablesOf("{\"ignoreCase\": true}"),
			findings:       1,
			fixed:          pointerTo("var a, d, [b, c] = {};"),
			declinesRepair: false,
		},
		{
			source:         "var d, a, [b, {x: {c, e}}] = {};",
			settings:       sortVariablesOf("{\"ignoreCase\": true}"),
			findings:       1,
			fixed:          pointerTo("var a, d, [b, {x: {c, e}}] = {};"),
			declinesRepair: false,
		},
		{
			source:         "var {} = 1, b, a",
			settings:       sortVariablesOf("{\"ignoreCase\": true}"),
			findings:       1,
			fixed:          pointerTo("var {} = 1, a, b"),
			declinesRepair: false,
		},
		{source: "var b=10, a=f();", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b=10, a=b;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = 0, a = `${b}`;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = 0, a = `${f()}`", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = 0, c = b, a;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = 0, c = 0, a = b + c;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = f(), c, d, a;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var b = `${f()}`, c, d, a;", findings: 1, fixed: nil, declinesRepair: true},
		{source: "var c, a = b = 0", findings: 1, fixed: nil, declinesRepair: true},
	}

	for _, testCase := range cases {
		result := rule_testing.RunGiven(test, SortVariables, sortVariablesFile, testCase.source, testCase.settings)
		if len(result.Diagnostics) != testCase.findings {
			test.Errorf("%q with %+v: expected %d findings, got %d %v",
				testCase.source, testCase.settings, testCase.findings,
				len(result.Diagnostics), result.MessageIds())
			continue
		}
		expected := make([]string, 0, testCase.findings)
		for range testCase.findings {
			expected = append(expected, messageSortVariablesSortVariables.Id)
		}
		rule_testing.ExpectFindings(test, result, expected...)

		proposed := 0
		for _, diagnostic := range result.Diagnostics {
			proposed += len(diagnostic.Fixes)
		}
		switch {
		case testCase.declinesRepair:
			if proposed != 0 {
				test.Errorf("%q: upstream declines to repair this, so the rule must propose no fix, got %d",
					testCase.source, proposed)
			}
		case testCase.fixed != nil:
			rule_testing.ExpectFixedSource(test, result, *testCase.fixed)
		}
	}
}

// TestSortVariablesStaysSilent carries upstream's clean cases.
func TestSortVariablesStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source   string
		settings rule.Given[SortVariablesSettings]
	}{
		{source: "var a=10, b=4, c='abc'"},
		{source: "var a, b, c, d"},
		{source: "var b; var a; var d;"},
		{source: "var _a, a"},
		{source: "var A, a"},
		{source: "var A, b"},
		{source: "var a, A;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var A, a;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var a, B, c;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var A, b, C;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var {a, b, c} = x;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var {A, b, C} = x;", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var test = [1,2,3];"},
		{source: "var {a,b} = [1,2];"},
		{source: "var [a, B, c] = [1, 2, 3];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var [A, B, c] = [1, 2, 3];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var [A, b, C] = [1, 2, 3];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "let {a, b, c} = x;"},
		{source: "let [a, b, c] = [1, 2, 3];"},
		{source: "const {a, b, c} = {a: 1, b: true, c: \"Moo\"};", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "const [a, b, c] = [1, true, \"Moo\"];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "const [c, a, b] = [1, true, \"Moo\"];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var {a, x: {b, c}} = {};"},
		{source: "var {c, x: {a, c}} = {};"},
		{source: "var {a, x: [b, c]} = {};"},
		{source: "var [a, {b, c}] = {};"},
		{source: "var [a, {x: {b, c}}] = {};"},
		{source: "var a = 42, {b, c } = {};"},
		{source: "var b = 42, {a, c } = {};"},
		{source: "var [b, {x: {a, c}}] = {};"},
		{source: "var [b, d, a, c] = {};"},
		{source: "var e, [a, c, d] = {};"},
		{source: "var a, [E, c, D] = [];", settings: sortVariablesOf("{\"ignoreCase\": true}")},
		{source: "var a, f, [e, c, d] = [1,2,3];"},
		{
			source: "export default class {\n    render () {\n        let {\n            b\n        } = this,\n            a,\n            c;\n    }\n}",
		},
		{source: "var {} = 1, a", settings: sortVariablesOf("{\"ignoreCase\": true}")},
	}

	for _, testCase := range cases {
		result := rule_testing.RunGiven(test, SortVariables, sortVariablesFile, testCase.source, testCase.settings)
		rule_testing.ExpectClean(test, result)
	}
}

// TestSortVariablesLiteralSetDecidesFixability covers what upstream's corpus cannot.
//
// The fixer declines the whole block when any identifier declarator has a non-literal initializer,
// because reordering `var b = foo(), a = bar();` would change evaluation order. "Literal" is
// upstream's ESTree node type, which is one type covering a string, a number, a boolean, `null` and
// a regular expression. Our parser gives each its own kind and models `true`/`false`/`null` as
// keywords, so the set has to be written out by hand, and a hand-written set is a place to be wrong
// in either direction.
//
// The corpus does not settle it: a mutation adding a template literal to the set survived all 61
// imported fixtures, because no corpus case pairs a template with an out-of-order name. Every row
// below is what the installed ESLint 10.8.1 rule produced, read from whether it attached a fix:
//
//	var b = 1, a = 2;      fix "a = 2, b = 1"       a number is a Literal
//	var b = /re/, a = 1;   fix "a = 1, b = /re/"    so is a regular expression
//	var b = null, a = 1;   fix "a = 1, b = null"    and null, which is a keyword here
//	var b = true, a = 1;   fix "a = 1, b = true"    and a boolean, likewise a keyword
//	var b = `x`, a = 1;    NO FIX                   a template is TemplateLiteral, not Literal
//
// The last row is the one the mutation needed. The four above it are the control: without them a
// rule that declined everything would pass the template row and prove nothing.
func TestSortVariablesLiteralSetDecidesFixability(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source string
		// fixed is the repair upstream attaches, or nil where it declines.
		fixed *string
	}{
		{source: "var b = 1, a = 2;", fixed: pointerTo("var a = 2, b = 1;")},
		{source: "var b = /re/, a = 1;", fixed: pointerTo("var a = 1, b = /re/;")},
		{source: "var b = null, a = 1;", fixed: pointerTo("var a = 1, b = null;")},
		{source: "var b = true, a = 1;", fixed: pointerTo("var a = 1, b = true;")},
		{source: "var b = 'str', a = 1;", fixed: pointerTo("var a = 1, b = 'str';")},
		// A template literal is not an ESTree Literal, so upstream reports and declines.
		{source: "var b = `x`, a = 1;", fixed: nil},
		// A call is the ordinary unfixable case, kept as the other side of the same gate.
		{source: "var b = foo(), a = 1;", fixed: nil},
	}

	for _, testCase := range cases {
		result := rule_testing.Run(test, SortVariables, sortVariablesFile, testCase.source)
		if len(result.Diagnostics) != 1 {
			test.Errorf("%q: expected exactly 1 finding, got %d %v",
				testCase.source, len(result.Diagnostics), result.MessageIds())
			continue
		}
		proposed := 0
		for _, diagnostic := range result.Diagnostics {
			proposed += len(diagnostic.Fixes)
		}
		if testCase.fixed == nil {
			if proposed != 0 {
				test.Errorf("%q: upstream declines to repair this, so the rule must propose no fix, got %d",
					testCase.source, proposed)
			}
			continue
		}
		rule_testing.ExpectFixedSource(test, result, *testCase.fixed)
	}
}

// TestSortVariablesRepairKeepsTheTextBetweenDeclarators covers the other half of the fixer that the
// corpus under-tests: where the moved text starts and what sits between the moved pieces.
//
// Upstream rebuilds the span from the first identifier declarator to the last and, for each sorted
// declarator, appends the original separator that followed the declarator in that position. Two
// ways to get that wrong produce valid-looking source:
//
//	joining with ", "        reflows the block and DELETES any comment inside it
//	starting at Pos()        drags the leading whitespace into the moved text, so it is
//	                         duplicated once per declarator
//
// `Pos()` is the trap specific to this tree: it starts at the previous token's end, so it includes
// leading trivia, where upstream's `range[0]` does not. Probed directly, a declarator's raw span is
// `" a"` and its token span is `"a"`.
//
// Every expectation is what the installed rule produced.
func TestSortVariablesRepairKeepsTheTextBetweenDeclarators(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source string
		fixed  string
	}{
		// A comment between declarators has to survive in its position, not travel with a name.
		{
			source: "var b /* first */, a /* second */;",
			fixed:  "var a /* first */, b /* second */;",
		},
		// A newline and its indentation are separator text too.
		{
			source: "var b = 1,\n    a = 2;",
			fixed:  "var a = 2,\n    b = 1;",
		},
		// Extra spacing around the comma is preserved rather than normalised.
		{
			source: "var b  ,  a;",
			fixed:  "var a  ,  b;",
		},
	}

	for _, testCase := range cases {
		result := rule_testing.Run(test, SortVariables, sortVariablesFile, testCase.source)
		if len(result.Diagnostics) == 0 {
			test.Errorf("%q: expected a finding, got none", testCase.source)
			continue
		}
		rule_testing.ExpectFixedSource(test, result, testCase.fixed)
	}
}
