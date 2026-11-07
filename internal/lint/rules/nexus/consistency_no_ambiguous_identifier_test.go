package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const ambiguousFile = "/repository/source/Thing.tsx"

func TestConsistencyNoAmbiguousIdentifierFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{"catch parameter", "try {\n    run();\n} catch (e) {\n    report();\n}\n", []string{"noAmbiguousE"}},
		{"single letter", "const n = 1;\n", []string{"noSingleLetter"}},
		{"a outside a comparator", "const a = 1;\n", []string{"noSingleLetter"}},
		{"b outside a comparator", "const b = 1;\n", []string{"noSingleLetter"}},
		// A name is judged where it is declared, once. Its reads are the same defect again, so a
		// name used three times is still one finding (am01, am06).
		{
			"e used in a body",
			"try {\n    run();\n} catch (e) {\n    report(e);\n}\n",
			[]string{"noAmbiguousE"},
		},
		{"a letter read twice", "export function twice(): number {\n    const n = 1;\n    return n + n;\n}\n",
			[]string{"noSingleLetter"}},
		{"a parameter", "export function double(n: number): number {\n    return n * 2;\n}\n",
			[]string{"noSingleLetter"}},
		{"a destructured binding", "const { n } = load();\nuse(n);\n", []string{"noSingleLetter"}},
		{"a destructured rename", "const { count: n } = load();\nuse(n);\n", []string{"noSingleLetter"}},
		{"a function", "function f() {}\nf();\n", []string{"noSingleLetter"}},
		{"a class property", "class Thing {\n    n = 1;\n}\n", []string{"noSingleLetter"}},
		{"a method", "class Thing {\n    m() {}\n}\n", []string{"noSingleLetter"}},
		{"an enum member", "enum Kind {\n    k,\n}\n", []string{"noSingleLetter"}},
		// A shorthand property reads the binding into a key spelled the same, and in an assignment
		// pattern writes one. Either way the binding is declared, and judged, elsewhere.
		{"a shorthand property", "const n = 1;\nconst value = { n };\n", []string{"noSingleLetter"}},
		{"a shorthand assignment pattern", "let n = 1;\n({ n } = load());\n", []string{"noSingleLetter"}},
		// A label binds no symbol, so TypeScript does not count it as a declaration. The labeled
		// statement is still where the name is chosen, and `continue` is its read.
		{"a label", "l: for (const value of values) {\n    continue l;\n}\n", []string{"noSingleLetter"}},
		// No comparator exemption and no numeric-kernel exemption, in any language (Kirk, 2026-11-06,
		// #4b337jk, call 3). `a` and `b` in a comparator are reported at their declarations, as any
		// single letter is, and so are a numeric function's letters (am03, am04, am05).
		{"sort comparator parameters", "const sorted = items.sort((a, b) => a - b);\n",
			[]string{"noSingleLetter", "noSingleLetter"}},
		{"sort comparator with a body", "const sorted = items.sort((a, b) => {\n    return a - b;\n});\n",
			[]string{"noSingleLetter", "noSingleLetter"}},
		{
			"sort comparator as a function expression",
			"const sorted = items.sort(function (a, b) {\n    return a - b;\n});\n",
			[]string{"noSingleLetter", "noSingleLetter"},
		},
		{
			"a sort comparator (am03)",
			"export function sortNumbers(values: number[]): number[] {\n  return values.sort((a, b) => a - b)\n}\n",
			[]string{"noSingleLetter", "noSingleLetter"},
		},
		{
			"a toSorted comparator (am04)",
			"export function sortedCopy(values: number[]): number[] {\n  return values.toSorted((a, b) => a - b)\n}\n",
			[]string{"noSingleLetter", "noSingleLetter"},
		},
		{
			"a numeric kernel (am05)",
			"export function mix(s: number, t: number): number {\n  const d = s - t\n  return d * t\n}\n",
			[]string{"noSingleLetter", "noSingleLetter", "noSingleLetter"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

func TestConsistencyNoAmbiguousIdentifierStaysSilent(test *testing.T) {
	test.Parallel()

	// The exemptions are the rule. Without them it fires on conventional spellings and on names
	// somebody else chose, which is how a naming rule gets switched off.
	cases := []struct {
		name       string
		sourceText string
	}{
		{"coordinates", "const x = 1;\nconst y = 2;\nconst z = 3;\n"},
		{"descriptive names", "const count = 1;\ntry {\n    run();\n} catch (error) {\n    report(error);\n}\n"},
		{
			"sort comparator parameters named for their side",
			"const sorted = items.sort((left, right) => left - right);\n",
		},
		// The bare `_` is the discard, the language's spelling for a value deliberately not named, so
		// it names nothing a reader has to track (am02).
		{"the bare underscore", "const _ = 1;\n"},
		{"the bare underscore as a parameter", "export const indexes = values.map((_, index) => index);\n"},
		// A read of a name declared elsewhere: the declaration is not in this file to judge.
		{"a read of a global", "export const total = use(n);\n"},
		{"a write to a global", "export function reset() {\n    n = 1;\n}\n"},
		{"a read of an imported name", "import { e } from 'x';\ne();\n"},

		// These are names somebody else chose, not bindings this file has to track.
		{"a property read", "const value = thing.e;\n"},
		{"a property read of a", "const value = thing.a;\n"},
		{"an object key", "const options = { e: 1, a: 2 };\n"},
		{"an import specifier", "import { e } from 'x';\n"},
		{"a type property signature", "interface ThingInterface {\n    e: string;\n    a: number;\n}\n"},
		// An uppercase single letter is a type parameter.
		{"type parameters", "export function identity<T>(value: T): T {\n    return value;\n}\n"},
		{"an underscore-prefixed name is fine", "const _event = 1;\n"},

		// JSX names. An intrinsic element is named by HTML rather than by us, so `<p>` and `<b>`
		// are not names anyone chose. Their absence from the fixtures let this rule report 3,081
		// false findings on the real tree before it was caught.
		{"a jsx intrinsic element", "export const Thing = () => <p>hi</p>;\n"},
		{"a nested jsx element", "export const Thing = () => <p><b>hi</b></p>;\n"},
		{"a self-closing jsx element", "export const Thing = () => <img src=\"x\" />;\n"},
		{"a jsx attribute name", "export const Thing = () => <div a=\"1\" />;\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// The whole point of the `e` message is saying which of the two things it thinks this is, so a
// reader can disagree with the reasoning rather than only the verdict.
func TestConsistencyNoAmbiguousIdentifierInfersContext(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantHint   string
		wantName   string
	}{
		{
			"catch reads as an error",
			"try {\n    run();\n} catch (e) {\n    report();\n}\n",
			"appears to be an error",
			"error",
		},
		{
			"a handler key reads as an event",
			"const properties = { onClick: (e) => run() };\n",
			"appears to be an event",
			"event",
		},
		{
			"a handler-named variable reads as an event",
			"const handleClick = (e) => run();\n",
			"appears to be an event",
			"event",
		},
		{"a bare arrow is unclear", "const start = (e) => run();\n", "context unclear", "event"},
		// The advice derives from the declaration. A read inside the catch body was judged on its
		// own once, found no catch above it, and called the same `e` an unclear event (am06).
		{
			"a catch-bound e read in its body is one error",
			"try {\n    run();\n} catch (e) {\n    return String(e);\n}\n",
			"appears to be an error",
			"error",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, ambiguousFile, testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("expected one finding, at the declaration, got %d", len(result.Diagnostics))
			}
			description := result.Diagnostics[0].Message.Description
			if !strings.Contains(description, testCase.wantHint) {
				test.Fatalf("expected hint %q, got: %s", testCase.wantHint, description)
			}
			if !strings.Contains(description, `"`+testCase.wantName+`"`) {
				test.Fatalf("expected suggestion %q, got: %s", testCase.wantName, description)
			}
		})
	}
}

// No fix, deliberately. Renaming a binding without following its references through scope would
// leave every other use pointing at a name that no longer exists.
func TestConsistencyNoAmbiguousIdentifierProposesNoFix(test *testing.T) {
	test.Parallel()

	result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, ambiguousFile,
		"try {\n    run();\n} catch (e) {\n    report(e);\n}\n")
	for _, diagnostic := range result.Diagnostics {
		if len(diagnostic.Fixes) != 0 {
			test.Fatalf("expected no fixes, got %d", len(diagnostic.Fixes))
		}
	}
}

// TestConsistencyNoAmbiguousIdentifierForeignNames pins the positions a name can occupy that this
// file did not choose.
//
// Every case here is a shape the rule reported before it adopted the shared ownership predicate,
// and none of them was covered by any fixture, which is why the change was invisible: the whole
// suite stayed green through it. The rule carried its own copy of the question, one word apart in
// name from the abbreviation rule's and four kinds apart in answer, and neither file could see the
// other.
//
// The binding position is written without a use site on purpose. A reference to an imported name is
// a different question, and including one would make these cases measure that instead.
func TestConsistencyNoAmbiguousIdentifierForeignNames(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name       string
		sourceText string
	}{
		{"a namespace import binding", "import * as e from './m';"},
		{"a default import binding", "import e from './m';"},
		{"a named import binding", "import { e } from './m';"},
		// The namespace is imported rather than declared here, so the only occurrence of `e` is the
		// right half of the qualified name. A locally declared `type e` inside the namespace is a
		// name this file owns and is correctly still reported, which would measure the wrong thing.
		{"the right half of a qualified type name",
			"import type * as N from './m';\ndeclare const v: N.e;"},
		{"a reference to an imported type", "import type { e } from './m';\ndeclare const v: e;"},
		{"a property read off another object", "declare const thing: any;\nthing.e;"},
		{"an object literal key", "({ e: 1 });"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, "p.ts", testCase.sourceText)
			for _, diagnostic := range result.Diagnostics {
				reported := testCase.sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported == "e" {
					test.Errorf("reported `e` in a position this file does not own: %q",
						testCase.sourceText)
				}
			}
		})
	}
}

// TestConsistencyNoAmbiguousIdentifierJudgesThisProperty is the other half of the same adoption,
// and the one place the two lifted implementations genuinely disagreed rather than merely differing
// in coverage.
//
// A property read off `this` belongs to the class being read, which this file owns, and its
// declaration is a property declaration that no member check protects. The abbreviation rule judges
// it and records a named production defect as its evidence: a class declared `maximumBackoff` while
// reading `this.maximumBackoff`. This rule exempted it, and now does not.
//
// The class's `e` is judged once, at its declaration. The `this.e` read is not reported, because no
// read is, not because the name is foreign.
func TestConsistencyNoAmbiguousIdentifierJudgesThisProperty(test *testing.T) {
	test.Parallel()

	const sourceText = "class C { e = 1; m() { return this.e; } }"
	result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, "p.ts", sourceText)
	var starts []int
	for _, diagnostic := range result.Diagnostics {
		if sourceText[diagnostic.Range.Pos():diagnostic.Range.End()] == "e" {
			starts = append(starts, diagnostic.Range.Pos())
		}
	}
	declaration := strings.Index(sourceText, "e = 1")
	if len(starts) != 1 || starts[0] != declaration {
		test.Errorf("expected the class's `e` once, at its declaration (offset %d), got %v", declaration, starts)
	}
}

// TestConsistencyNoAmbiguousIdentifierJudgesAJavaScriptThisProperty pins the declaration JavaScript
// has and TypeScript does not: a class may create a property by assigning it, and that assignment is
// the property's declaration. Judging declarations only would otherwise leave a property made that way
// unreported anywhere. Each want is the text that starts where a finding should, so a finding on the
// wrong `e` fails as surely as a missing one.
func TestConsistencyNoAmbiguousIdentifierJudgesAJavaScriptThisProperty(test *testing.T) {
	test.Parallel()

	for _, testCase := range []struct {
		name       string
		fileName   string
		sourceText string
		want       []string
	}{
		{
			"an assignment in the constructor reports once, at the first",
			"/repository/source/Thing.js",
			"export class Thing {\n    constructor() {\n        this.e = 1;\n    }\n" +
				"    reset() {\n        this.e = 2;\n        return this.e;\n    }\n}\n",
			[]string{"e = 1;"},
		},
		{
			"a declared field is the declaration, and the assignment is not",
			"/repository/source/Thing.js",
			"export class Thing {\n    e = 0;\n    constructor() {\n        this.e = 1;\n    }\n}\n",
			[]string{"e = 0;"},
		},
		{
			"a declared method is the declaration, and the assignment is not",
			"/repository/source/Thing.js",
			"export class Thing {\n    constructor() {\n        this.e = run;\n    }\n    e() {}\n}\n",
			[]string{"e() {}"},
		},
		// An arrow function keeps the class's `this`, and a function expression has its own, which
		// the binder does not bind onto the class.
		{
			"an arrow in the constructor keeps the class's this",
			"/repository/source/Thing.js",
			"export class Thing {\n    constructor() {\n        run(() => {\n            this.e = 1;\n        });\n    }\n}\n",
			[]string{"e = 1;"},
		},
		{
			"a function expression's this is not the class's",
			"/repository/source/Thing.js",
			"export class Thing {\n    constructor() {\n        run(function () {\n            this.e = 1;\n        });\n    }\n}\n",
			nil,
		},
		// A static member's `this` is the class, so its property is a different name from the
		// instance's, as the binder keeps them apart.
		{
			"a static property and an instance property are two names",
			"/repository/source/Thing.js",
			"export class Thing {\n    static create() {\n        this.e = 1;\n    }\n" +
				"    constructor() {\n        this.e = 2;\n    }\n}\n",
			[]string{"e = 1;", "e = 2;"},
		},
		// TypeScript requires the member, so the assignment declares nothing and is a write.
		{
			"the same assignment in TypeScript reports nothing",
			"/repository/source/Thing.ts",
			"export class Thing {\n    constructor() {\n        this.e = 1;\n    }\n" +
				"    reset() {\n        this.e = 2;\n        return this.e;\n    }\n}\n",
			nil,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoAmbiguousIdentifier, testCase.fileName, testCase.sourceText)
			var got []int
			for _, diagnostic := range result.Diagnostics {
				got = append(got, diagnostic.Range.Pos())
			}
			var want []int
			for _, text := range testCase.want {
				want = append(want, strings.Index(testCase.sourceText, text))
			}
			if len(got) != len(want) {
				test.Fatalf("expected findings at %v, got %v", want, got)
			}
			for index := range want {
				if got[index] != want[index] {
					test.Fatalf("expected findings at %v, got %v", want, got)
				}
			}
		})
	}
}
