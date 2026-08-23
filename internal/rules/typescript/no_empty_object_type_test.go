package typescript

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const emptyObjectFile = "/repository/source/Thing.ts"

// Seeded from oxc's own pass and fail vectors. The two cases that decide this rule — an interface
// extending two names, and `{}` inside an intersection — are both in that corpus and neither would
// have been invented here.

func TestNoEmptyObjectTypeFiresOnInterfaces(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"an empty interface", "export interface Base {}\n"},
		// Extending exactly one name is an alias written the long way, and oxc flags it under the
		// default `allowInterfaces: never`.
		{"extending a single name", "interface Base {\n    name: string;\n}\nexport interface Derived extends Base {}\n"},
		{"extending a generic", "export interface Base extends Array<number> {}\n"},
		{"a generic extending a generic", "interface Derived<T> {\n    value: T;\n}\nexport interface Base<T> extends Derived<T> {}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, NoEmptyObjectType, emptyObjectFile, testCase.sourceText),
				"noEmptyInterface")
		})
	}
}

func TestNoEmptyObjectTypeFiresOnObjectTypes(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a type alias", "export type Base = {};\n"},
		{"an annotation", "export let value: {};\n"},
		{"inside a union", "export type MyUnion<T> = T | {};\n"},
		{"unioned with null", "export type Base = {} | null;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, NoEmptyObjectType, emptyObjectFile, testCase.sourceText),
				"noEmptyObjectType")
		})
	}
}

// TestNoEmptyObjectTypeOffersTwoSuggestions pins that the repair is a choice rather than a fix.
//
// `object` and `unknown` mean different things and only the author knows which was meant, so a rule
// that applied one unattended would be changing the type rather than repairing a spelling. Asserting
// the count is what keeps a later change from quietly collapsing them into one automatic fix.
func TestNoEmptyObjectTypeOffersTwoSuggestions(t *testing.T) {
	result := ruletest.Run(t, NoEmptyObjectType, emptyObjectFile, "export let value: {};\n")
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected one finding, got %d", len(result.Diagnostics))
	}
	if got := len(result.Diagnostics[0].Suggestions); got != 2 {
		t.Fatalf("expected two suggestions, got %d", got)
	}
	if got := len(result.Diagnostics[0].Fixes); got != 0 {
		t.Fatalf("expected no automatic fix, got %d", got)
	}
}

func TestNoEmptyObjectTypeStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"an interface with members", "export interface Base {\n    name: string;\n}\n"},
		// The case the whole rule turns on, and it is oxc's comment that explains why: extending
		// multiple interfaces is how a reader expresses an intersection of named types where a union
		// would be wrong. Two or more is allowed; exactly one is not.
		{"extending two names", "interface Base {\n    name: string;\n}\ninterface Derived {\n    age: number;\n}\nexport interface Both extends Base, Derived {}\n"},
		{"extending three names", "interface A {\n    a: string;\n}\ninterface B {\n    b: string;\n}\ninterface C {\n    c: string;\n}\nexport interface All extends A, B, C {}\n"},
		// `Base & {}` forces a type to display expanded rather than by name. The empty half is doing
		// real work, which is why oxc exempts it.
		{"inside an intersection", "interface Base {\n    name: string;\n}\nexport type Expanded = Base & {};\n"},
		// A non-empty object type is not this rule's concern in either position.
		{"a populated type literal", "export type Base = { name: string };\n"},
		{"a populated annotation", "export let value: { name: string };\n"},
		// An empty *object literal* is a value, not a type. A rule keyed to braces rather than to
		// type nodes would fire on every one of these.
		{"an empty object value", "export const Value = {};\n"},
		{"an empty object argument", "export const Result = JSON.stringify({});\n"},
		{"an empty destructuring default", "export function run({ a } = { a: 1 }) {\n    return a;\n}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoEmptyObjectType, emptyObjectFile, testCase.sourceText))
		})
	}
}
