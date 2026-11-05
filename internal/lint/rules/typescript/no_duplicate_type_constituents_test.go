package typescript

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const noDuplicateTypeConstituentsFile = "/repository/source/Types.ts"

func noDuplicateTypeConstituentsCaseName(index int) string {
	return "case" + strconv.Itoa(index)
}

// noDuplicateTypeConstituentsFinding is one expected finding with every layer it can be wrong at.
//
// The message is asserted in full because it interpolates: the duplicate message names both the
// union-or-intersection word and the previous constituent's own text, so one rule renders many
// different strings and a message-id assertion cannot see any of them.
type noDuplicateTypeConstituentsFinding struct {
	wantId      string
	wantSpan    string
	wantMessage string
}

// noDuplicateTypeConstituentsOptionsFor routes a case's options through the rule's own decoder.
func noDuplicateTypeConstituentsOptionsFor(test *testing.T, optionsJson string) NoDuplicateTypeConstituentsOptions {
	test.Helper()
	if optionsJson == "" {
		return DefaultNoDuplicateTypeConstituentsSettings()
	}
	decoded, decodeNoDuplicateTypeConstituentsOptionsError := DecodeNoDuplicateTypeConstituentsOptions(
		json.RawMessage(optionsJson),
	)
	if decodeNoDuplicateTypeConstituentsOptionsError != nil {
		test.Fatalf("decoding %s: %v", optionsJson, decodeNoDuplicateTypeConstituentsOptionsError)
	}
	return decoded
}

// TestNoDuplicateTypeConstituentsStaysSilentOnUpstreamPassCases is the imported clean corpus.
//
// All thirty-four of upstream's passing inputs, extracted by parsing the clone's test file with the
// TypeScript compiler. Every one was additionally run through the installed 8.x build against a
// real program, which reported nothing and produced no parse error on any of them.
//
// Two of the thirty-four carry options, one per key, so both halves of the option surface are
// exercised in the direction that turns the rule off.
func TestNoDuplicateTypeConstituentsStaysSilentOnUpstreamPassCases(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText  string
		optionsJson string
	}{
		{sourceText: "type T = 1 | 2;", optionsJson: ""},
		{sourceText: "type T = 1 | '1';", optionsJson: ""},
		{sourceText: "type T = true & boolean;", optionsJson: ""},
		{sourceText: "type T = null | undefined;", optionsJson: ""},
		{sourceText: "type T = any | unknown;", optionsJson: ""},
		{sourceText: "type T = { a: string } | { b: string };", optionsJson: ""},
		{sourceText: "type T = { a: string; b: number } | { b: number; a: string };", optionsJson: ""},
		{sourceText: "type T = { a: string | number };", optionsJson: ""},
		{sourceText: "type T = Set<string> | Set<number>;", optionsJson: ""},
		{sourceText: "type T = Class<string> | Class<number>;", optionsJson: ""},
		{sourceText: "type T = string[] | number[];", optionsJson: ""},
		{sourceText: "type T = string[][] | string[];", optionsJson: ""},
		{sourceText: "type T = [1, 2, 3] | [1, 2, 4];", optionsJson: ""},
		{sourceText: "type T = [1, 2, 3] | [1, 2, 3, 4];", optionsJson: ""},
		{sourceText: "type T = 'A' | string[];", optionsJson: ""},
		{sourceText: "type T = (() => string) | (() => void);", optionsJson: ""},
		{sourceText: "type T = () => string | void;", optionsJson: ""},
		{sourceText: "type T = () => null | undefined;", optionsJson: ""},
		{sourceText: "type T = (arg: string | number) => void;", optionsJson: ""},
		{sourceText: "type T = A | A;", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype T = A | B;\n      ", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\nconst a: A | B = 'A';\n      ", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype T = A | /* comment */ B;\n      ", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype T = 'A' | 'B';\n      ", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype T = A | B | C;\n      ", optionsJson: ""},
		{sourceText: "type T = readonly string[] | string[];", optionsJson: ""},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype D = 'D';\ntype T = (A | B) | (C | D);\n      ",
			optionsJson: "",
		},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype T = (A | B) | (A & B);\n      ", optionsJson: ""},
		{sourceText: "\ntype A = 'A';\ntype B = 'B';\ntype T = Record<string, A | B>;\n      ", optionsJson: ""},
		{sourceText: "type T = A | A;", optionsJson: "{\"ignoreUnions\": true}"},
		{sourceText: "type T = A & A;", optionsJson: "{\"ignoreIntersections\": true}"},
		{sourceText: "type T = Class<string> | Class<string>;", optionsJson: ""},
		{sourceText: "type T = A | A | string;", optionsJson: ""},
		{sourceText: "(a: string | undefined) => {};", optionsJson: ""},
	}
	for index, testCase := range cases {
		test.Run(noDuplicateTypeConstituentsCaseName(index), func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
				noDuplicateTypeConstituentsFile, testCase.sourceText,
				noDuplicateTypeConstituentsOptionsFor(test, testCase.optionsJson)))
		})
	}
}

// TestNoDuplicateTypeConstituentsFiresOnUpstreamFailCases is the imported reporting corpus.
//
// All forty-eight of upstream's failing inputs, carrying fifty-seven findings between them across
// both message ids. Every span and every rendered message was measured against the installed build
// rather than transcribed.
//
// The span assertions matter more here than on most rules, because the reported range is not the
// range the repair removes. A duplicate written as a parenthesized nesting underlines text starting
// inside the parenthesis and ending after the closing bracket, so `A | (A | A)` reports a span
// reading as an unbalanced fragment. That is upstream's choice and only a span fixture records it.
//
// The whole-file rewrite is asserted on every row against the text upstream's own `output` records,
// transformed the way the harness transforms the input. One upstream case carries an `output` array
// of two states because its repairs converge over two passes; the harness applies one pass, so that
// row asserts the first state and is called out at its line.
func TestNoDuplicateTypeConstituentsFiresOnUpstreamFailCases(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText   string
		optionsJson  string
		wantFixed    string
		wantFindings []noDuplicateTypeConstituentsFinding
	}{
		{
			sourceText:  "type T = 1 | 1;",
			optionsJson: "",
			wantFixed:   "type T = 1  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "1", wantMessage: "Union type constituent is duplicated with 1."},
			},
		},
		{
			sourceText:  "type T = true & true;",
			optionsJson: "",
			wantFixed:   "type T = true  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "true",
					wantMessage: "Intersection type constituent is duplicated with true.",
				},
			},
		},
		{
			sourceText:  "type T = null | null;",
			optionsJson: "",
			wantFixed:   "type T = null  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "null", wantMessage: "Union type constituent is duplicated with null."},
			},
		},
		{
			sourceText:  "type T = any | any;",
			optionsJson: "",
			wantFixed:   "type T = any  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "any", wantMessage: "Union type constituent is duplicated with any."},
			},
		},
		{
			sourceText:  "type T = { a: string | string };",
			optionsJson: "",
			wantFixed:   "type T = { a: string   };\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "type T = { a: string } | { a: string };",
			optionsJson: "",
			wantFixed:   "type T = { a: string }  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "{ a: string }",
					wantMessage: "Union type constituent is duplicated with { a: string }.",
				},
			},
		},
		{
			sourceText:  "type T = { a: string; b: number } | { a: string; b: number };",
			optionsJson: "",
			wantFixed:   "type T = { a: string; b: number }  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "{ a: string; b: number }",
					wantMessage: "Union type constituent is duplicated with { a: string; b: number }.",
				},
			},
		},
		{
			sourceText:  "type T = Set<string> | Set<string>;",
			optionsJson: "",
			wantFixed:   "type T = Set<string>  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "Set<string>",
					wantMessage: "Union type constituent is duplicated with Set<string>.",
				},
			},
		},
		{
			sourceText:  "\ntype IsArray<T> = T extends any[] ? true : false;\ntype ActuallyDuplicated = IsArray<number> | IsArray<string>;\n      ",
			optionsJson: "",
			wantFixed:   "type IsArray<T> = T extends any[] ? true : false;\ntype ActuallyDuplicated = IsArray<number>  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "IsArray<string>",
					wantMessage: "Union type constituent is duplicated with IsArray<number>.",
				},
			},
		},
		{
			sourceText:  "type T = string[] | string[];",
			optionsJson: "",
			wantFixed:   "type T = string[]  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string[]",
					wantMessage: "Union type constituent is duplicated with string[].",
				},
			},
		},
		{
			sourceText:  "type T = string[][] | string[][];",
			optionsJson: "",
			wantFixed:   "type T = string[][]  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string[][]",
					wantMessage: "Union type constituent is duplicated with string[][].",
				},
			},
		},
		{
			sourceText:  "type T = [1, 2, 3] | [1, 2, 3];",
			optionsJson: "",
			wantFixed:   "type T = [1, 2, 3]  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "[1, 2, 3]",
					wantMessage: "Union type constituent is duplicated with [1, 2, 3].",
				},
			},
		},
		{
			sourceText:  "type T = () => string | string;",
			optionsJson: "",
			wantFixed:   "type T = () => string  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "type T = () => null | null;",
			optionsJson: "",
			wantFixed:   "type T = () => null  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "null", wantMessage: "Union type constituent is duplicated with null."},
			},
		},
		{
			sourceText:  "type T = (arg: string | string) => void;",
			optionsJson: "",
			wantFixed:   "type T = (arg: string  ) => void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "type T = 'A' | 'A';",
			optionsJson: "",
			wantFixed:   "type T = 'A'  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "'A'", wantMessage: "Union type constituent is duplicated with 'A'."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype T = A | A;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype T = A  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\nconst a: A | A = 'A';\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\nconst a: A   = 'A';\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype T = A | /* comment */ A;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype T = A  /* comment */ ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A1 = 'A';\ntype A2 = 'A';\ntype A3 = 'A';\ntype T = A1 | A2 | A3;\n      ",
			optionsJson: "",
			wantFixed:   "type A1 = 'A';\ntype A2 = 'A';\ntype A3 = 'A';\ntype T = A1    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A2", wantMessage: "Union type constituent is duplicated with A1."},
				{wantId: "duplicate", wantSpan: "A3", wantMessage: "Union type constituent is duplicated with A1."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype T = A | B | A;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype T = A | B  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype T = A | B | A | B;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype T = A | B    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
				{wantId: "duplicate", wantSpan: "B", wantMessage: "Union type constituent is duplicated with B."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype T = A | B | A | A;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype T = A | B    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype T = A | B | A | C;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype T = A | B   | C;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype T = (A | B) | (A | B);\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype T = (A | B)  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "A | B)",
					wantMessage: "Union type constituent is duplicated with A | B.",
				},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype T = A | (A | A);\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype T = A  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A | A)", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype D = 'D';\ntype F = (A | B) | (A | B) | ((C | D) & (A | B)) | (A | B);\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype C = 'C';\ntype D = 'D';\ntype F = (A | B)   | ((C | D) & (A | B))  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "A | B)",
					wantMessage: "Union type constituent is duplicated with A | B.",
				},
				{
					wantId:      "duplicate",
					wantSpan:    "A | B)",
					wantMessage: "Union type constituent is duplicated with A | B.",
				},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype B = 'B';\ntype C = (A | B) | A | B | (A | B);\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype B = 'B';\ntype C = (A | B)      ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
				{wantId: "duplicate", wantSpan: "B", wantMessage: "Union type constituent is duplicated with B."},
				{
					wantId:      "duplicate",
					wantSpan:    "A | B)",
					wantMessage: "Union type constituent is duplicated with A | B.",
				},
			},
		},
		{
			sourceText:  "type A = (number | string) | number | string;",
			optionsJson: "",
			wantFixed:   "type A = (number | string)    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "number",
					wantMessage: "Union type constituent is duplicated with number.",
				},
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "type A = (number | (string | null)) | (string | (null | number));",
			optionsJson: "",
			wantFixed:   "type A = (number | (string | null))  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string | (null | number))",
					wantMessage: "Union type constituent is duplicated with number | (string | null).",
				},
			},
		},
		{
			sourceText:  "type A = (number & string) & number & string;",
			optionsJson: "",
			wantFixed:   "type A = (number & string)    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "number",
					wantMessage: "Intersection type constituent is duplicated with number.",
				},
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Intersection type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "type A = number & string & (number & string);",
			optionsJson: "",
			wantFixed:   "", // repairs overlap; see the note at the assertion below
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "number",
					wantMessage: "Intersection type constituent is duplicated with number.",
				},
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Intersection type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "\ntype A = 'A';\ntype T = Record<string, A | A>;\n      ",
			optionsJson: "",
			wantFixed:   "type A = 'A';\ntype T = Record<string, A  >;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A", wantMessage: "Union type constituent is duplicated with A."},
			},
		},
		{
			sourceText:  "type T = A | A | string | string;",
			optionsJson: "",
			wantFixed:   "type T = A | A | string  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			sourceText:  "(a?: string | undefined) => {};",
			optionsJson: "",
			wantFixed:   "(a?: string  ) => {};\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\ntype T = undefined;\n(arg?: T | string) => {};\n      ",
			optionsJson: "",
			wantFixed:   "type T = undefined;\n(arg?:   string) => {};\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "T",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\ninterface F {\n  (a?: string | undefined): void;\n}\n      ",
			optionsJson: "",
			wantFixed:   "interface F {\n  (a?: string  ): void;\n}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "type fn = new (a?: string | undefined) => void;",
			optionsJson: "",
			wantFixed:   "type fn = new (a?: string  ) => void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "function f(a?: string | undefined) {}",
			optionsJson: "",
			wantFixed:   "function f(a?: string  ) {}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "f = function (a?: string | undefined) {};",
			optionsJson: "",
			wantFixed:   "f = function (a?: string  ) {};\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "declare function f(a?: string | undefined): void;",
			optionsJson: "",
			wantFixed:   "declare function f(a?: string  ): void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\ndeclare class bb {\n  f(a?: string | undefined): void;\n}\n      ",
			optionsJson: "",
			wantFixed:   "declare class bb {\n  f(a?: string  ): void;\n}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\ninterface ee {\n  f(a?: string | undefined): void;\n}\n      ",
			optionsJson: "",
			wantFixed:   "interface ee {\n  f(a?: string  ): void;\n}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\ninterface ee {\n  new (a?: string | undefined): void;\n}\n      ",
			optionsJson: "",
			wantFixed:   "interface ee {\n  new (a?: string  ): void;\n}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "type fn = (a?: string | undefined) => void;",
			optionsJson: "",
			wantFixed:   "type fn = (a?: string  ) => void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "type fn = (a?: string | (undefined | number)) => void;",
			optionsJson: "",
			wantFixed:   "type fn = (a?: string | (  number)) => void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "type fn = (a?: (undefined | number) | string) => void;",
			optionsJson: "",
			wantFixed:   "type fn = (a?: (  number) | string) => void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			sourceText:  "\nabstract class cc {\n  abstract f(a?: string | undefined): void;\n}\n      ",
			optionsJson: "",
			wantFixed:   "abstract class cc {\n  abstract f(a?: string  ): void;\n}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
	}
	for index, testCase := range cases {
		test.Run(noDuplicateTypeConstituentsCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
				noDuplicateTypeConstituentsFile, testCase.sourceText,
				noDuplicateTypeConstituentsOptionsFor(test, testCase.optionsJson))

			wantIds := make([]string, len(testCase.wantFindings))
			for position, want := range testCase.wantFindings {
				wantIds[position] = want.wantId
			}
			rule_testing.ExpectFindings(test, result, wantIds...)

			onDisk := strings.TrimSpace(testCase.sourceText) + "\n"
			for position, want := range testCase.wantFindings {
				diagnostic := result.Diagnostics[position]

				gotSpan := onDisk[diagnostic.Range.Pos():diagnostic.Range.End()]
				if gotSpan != want.wantSpan {
					test.Fatalf("finding %d span: expected %q, got %q", position, want.wantSpan, gotSpan)
				}
				if diagnostic.Message.Description != want.wantMessage {
					test.Fatalf(
						"finding %d message: expected %q, got %q",
						position,
						want.wantMessage,
						diagnostic.Message.Description,
					)
				}

				// The repair is a fix rather than a suggestion, matching upstream's fixable meta.
				// Removing a duplicate constituent cannot change what the type means.
				if len(diagnostic.Suggestions) != 0 {
					test.Fatalf("finding %d: expected a fix, got %d suggestions", position, len(diagnostic.Suggestions))
				}
				// One finding carries one or two removals: the separator token and the
				// constituent are removed independently so that the whitespace between them
				// survives, which is what upstream's recorded outputs show. The exact bytes are
				// asserted by the whole-file comparison below rather than by counting edits.
				if len(diagnostic.Fixes) == 0 {
					test.Fatalf("finding %d: expected a repair, got none", position)
				}
			}

			// An empty wantFixed marks the one case whose two repairs overlap, so there is no
			// single-pass rewrite to assert. Both findings remove the same separator inside the
			// parentheses; upstream's engine applies one per pass and re-runs, which is why the
			// corpus records that case with an output array of two states rather than a string.
			// Measured on the installed build: its two fix ranges are [28,36] and [35,43], which
			// share index 35. The harness refuses to guess which wins, correctly, so this row
			// asserts the findings and leaves the rewrite to the engine.
			if testCase.wantFixed != "" {
				rule_testing.ExpectFixedSource(test, result, testCase.wantFixed)
			}
		})
	}
}

// TestNoDuplicateTypeConstituentsOnTypeScriptShapesUpstreamDoesNotWrite is the added corpus.
//
// Upstream's corpus is thorough about unions and thin about the TypeScript syntax that can surround
// one, and this project has twice shipped a fixer that destroyed type information while passing
// every upstream output case. That cannot happen here for a structural reason worth stating: this
// repair only removes a separator and a constituent, and never rebuilds a span, so there is no
// annotation for it to strand. These cases are what establishes that rather than asserting it.
//
// Every verdict, span, message and rewrite below was measured against the installed 8.x build over
// a real program, and all fifteen agree with this port on all four.
func TestNoDuplicateTypeConstituentsStaysSilentOnTypeScriptShapes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
	}{
		{
			// An unresolvable import type resolves to the error type, which upstream skips so that a file with a broken
			// import does not report every constituent against the first.
			sourceText: "type T = import('x').A | import('x').A;",
		},
	}
	for index, testCase := range cases {
		test.Run(noDuplicateTypeConstituentsCaseName(index), func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
				noDuplicateTypeConstituentsFile, testCase.sourceText,
				DefaultNoDuplicateTypeConstituentsSettings()))
		})
	}
}

func TestNoDuplicateTypeConstituentsFiresOnTypeScriptShapes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText   string
		wantFixed    string
		wantFindings []noDuplicateTypeConstituentsFinding
	}{
		{
			// A readonly array modifier survives the repair, because the repair only removes and never rebuilds.
			sourceText: "type T = readonly string[] | readonly string[];",
			wantFixed:  "type T = readonly string[]  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "readonly string[]",
					wantMessage: "Union type constituent is duplicated with readonly string[].",
				},
			},
		},
		{
			// Two structurally identical object literals. The checker gives these distinct type objects, so only the
			// syntactic comparison can see them, which is why upstream runs it first.
			sourceText: "type T = { readonly a: 1 } | { readonly a: 1 };",
			wantFixed:  "type T = { readonly a: 1 }  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "{ readonly a: 1 }",
					wantMessage: "Union type constituent is duplicated with { readonly a: 1 }.",
				},
			},
		},
		{
			// The unnecessary-undefined message beside a second parameter that must not be touched.
			sourceText: "function f(a?: string | undefined, b?: number) {}",
			wantFixed:  "function f(a?: string  , b?: number) {}\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			// A method's optional parameter, with a return annotation the repair must leave alone.
			sourceText: "class K { m(a?: string | undefined): void {} }",
			wantFixed:  "class K { m(a?: string  ): void {} }\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			// A parenthesized function type. The span starts inside the bracket, which is the estree asymmetry.
			sourceText: "type T = (() => void) | (() => void);",
			wantFixed:  "type T = (() => void)  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "() => void)",
					wantMessage: "Union type constituent is duplicated with () => void.",
				},
			},
		},
		{
			// Three constituents, so two findings, and their repairs do not overlap.
			sourceText: "type T = string | string | string;",
			wantFixed:  "type T = string    ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			// A type parameter as the constituent.
			sourceText: "type G<T> = T | T;",
			wantFixed:  "type G<T> = T  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "T", wantMessage: "Union type constituent is duplicated with T."},
			},
		},
		{
			// A template literal type.
			sourceText: "type T = `a${string}` | `a${string}`;",
			wantFixed:  "type T = `a${string}`  ;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "`a${string}`",
					wantMessage: "Union type constituent is duplicated with `a${string}`.",
				},
			},
		},
		{
			// Inside an interface member rather than a type alias.
			sourceText: "interface I { a: string | string; }",
			wantFixed:  "interface I { a: string  ; }\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			// A variable annotation, where the initializer must survive.
			sourceText: "let x: string | string = 'a';",
			wantFixed:  "let x: string   = 'a';\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "string",
					wantMessage: "Union type constituent is duplicated with string.",
				},
			},
		},
		{
			// An ambient declaration.
			sourceText: "declare function f(a?: A | undefined): void;",
			wantFixed:  "declare function f(a?: A  ): void;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "unnecessary",
					wantSpan:    "undefined",
					wantMessage: "Explicit undefined is unnecessary on an optional parameter.",
				},
			},
		},
		{
			// An array of a named type.
			sourceText: "type T = A[] | A[];\ntype A = string;",
			wantFixed:  "type T = A[]  ;\ntype A = string;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "A[]", wantMessage: "Union type constituent is duplicated with A[]."},
			},
		},
		{
			// A keyof operator type.
			sourceText: "type T = keyof A | keyof A;\ntype A = {a:1};",
			wantFixed:  "type T = keyof A  ;\ntype A = {a:1};\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{
					wantId:      "duplicate",
					wantSpan:    "keyof A",
					wantMessage: "Union type constituent is duplicated with keyof A.",
				},
			},
		},
		{
			// A union in the false branch of a conditional type, where the enclosing conditional must not be disturbed.
			sourceText: "type T = A extends B ? C : D | D;\ntype A=1;type B=1;type C=1;type D=1;",
			wantFixed:  "type T = A extends B ? C : D  ;\ntype A=1;type B=1;type C=1;type D=1;\n",
			wantFindings: []noDuplicateTypeConstituentsFinding{
				{wantId: "duplicate", wantSpan: "D", wantMessage: "Union type constituent is duplicated with D."},
			},
		},
	}
	for index, testCase := range cases {
		test.Run(noDuplicateTypeConstituentsCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
				noDuplicateTypeConstituentsFile, testCase.sourceText,
				DefaultNoDuplicateTypeConstituentsSettings())

			wantIds := make([]string, len(testCase.wantFindings))
			for position, want := range testCase.wantFindings {
				wantIds[position] = want.wantId
			}
			rule_testing.ExpectFindings(test, result, wantIds...)

			onDisk := strings.TrimSpace(testCase.sourceText) + "\n"
			for position, want := range testCase.wantFindings {
				diagnostic := result.Diagnostics[position]
				gotSpan := onDisk[diagnostic.Range.Pos():diagnostic.Range.End()]
				if gotSpan != want.wantSpan {
					test.Fatalf("finding %d span: expected %q, got %q", position, want.wantSpan, gotSpan)
				}
				if diagnostic.Message.Description != want.wantMessage {
					test.Fatalf(
						"finding %d message: expected %q, got %q",
						position,
						want.wantMessage,
						diagnostic.Message.Description,
					)
				}
			}

			rule_testing.ExpectFixedSource(test, result, testCase.wantFixed)
		})
	}
}

// TestDecodeNoDuplicateTypeConstituentsOptions pins the decoder against its defaults.
//
// Both keys default to false, so the generic decoder would be right by coincidence. The
// hand-rolled one is tested anyway, because the coincidence is a property of today's defaults
// rather than of the option surface, and a sibling rule in this package had to be repaired for
// exactly the inverse case.
func TestDecodeNoDuplicateTypeConstituentsOptions(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name              string
		raw               string
		wantIntersections bool
		wantUnions        bool
	}{
		{name: "emptyObject", raw: `{}`, wantIntersections: false, wantUnions: false},
		{name: "ignoreUnions", raw: `{"ignoreUnions": true}`, wantIntersections: false, wantUnions: true},
		{name: "ignoreIntersections", raw: `{"ignoreIntersections": true}`, wantIntersections: true, wantUnions: false},
		{
			name:              "both",
			raw:               `{"ignoreUnions": true, "ignoreIntersections": true}`,
			wantIntersections: true,
			wantUnions:        true,
		},
		{name: "explicitFalse", raw: `{"ignoreUnions": false}`, wantIntersections: false, wantUnions: false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeNoDuplicateTypeConstituentsOptionsError := DecodeNoDuplicateTypeConstituentsOptions(
				json.RawMessage(testCase.raw),
			)
			if decodeNoDuplicateTypeConstituentsOptionsError != nil {
				test.Fatalf("decoding %s: %v", testCase.raw, decodeNoDuplicateTypeConstituentsOptionsError)
			}
			if decoded.IgnoreIntersections != testCase.wantIntersections {
				test.Fatalf(
					"ignoreIntersections: expected %v, got %v",
					testCase.wantIntersections,
					decoded.IgnoreIntersections,
				)
			}
			if decoded.IgnoreUnions != testCase.wantUnions {
				test.Fatalf("ignoreUnions: expected %v, got %v", testCase.wantUnions, decoded.IgnoreUnions)
			}
		})
	}
}

// TestNoDuplicateTypeConstituentsFallsBackToTheDefaultOnNilOptions pins the nil-options path.
//
// A rule configured as a bare "error" is handed nil options, and every fixture above reaches the
// rule through the decoder, so nothing there can see the fallback. Both defaults are false, so the
// separating input is any duplicate at all: under the fallback it reports, and a rule that had lost
// its options entirely would too, which is why the controls below turn each half off and confirm
// the silence moves.
func TestNoDuplicateTypeConstituentsFallsBackToTheDefaultOnNilOptions(test *testing.T) {
	test.Parallel()

	const union = "type T = A | A;\ntype A = string;"
	const intersection = "type T = A & A;\ntype A = { a: 1 };"

	rule_testing.ExpectFindings(test, rule_testing.RunTyped(test, NoDuplicateTypeConstituents,
		noDuplicateTypeConstituentsFile, union), "duplicate")
	rule_testing.ExpectFindings(test, rule_testing.RunTyped(test, NoDuplicateTypeConstituents,
		noDuplicateTypeConstituentsFile, intersection), "duplicate")

	// The controls: each half must go silent when its own key is set, so the reports above are the
	// defaults being applied rather than the options being ignored.
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
		noDuplicateTypeConstituentsFile, union,
		NoDuplicateTypeConstituentsOptions{IgnoreUnions: true}))
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test, NoDuplicateTypeConstituents,
		noDuplicateTypeConstituentsFile, intersection,
		NoDuplicateTypeConstituentsOptions{IgnoreIntersections: true}))
}

// TestNoDuplicateTypeConstituentsNeedsTheTypedHarness pins the checker declaration.
//
// Under the plain harness the checker is nil and every listener returns immediately, so the clean
// fixtures would pass having proven nothing. The syntactic comparison alone would still catch
// `A | A`, which makes the vacuous green here especially convincing.
func TestNoDuplicateTypeConstituentsNeedsTheTypedHarness(test *testing.T) {
	test.Parallel()

	if !NoDuplicateTypeConstituents.NeedsTypeChecker {
		test.Fatal("the rule compares constituent types by identity, so it must declare NeedsTypeChecker")
	}

	rule_testing.ExpectClean(test, rule_testing.Run(test, NoDuplicateTypeConstituents,
		noDuplicateTypeConstituentsFile, "type T = A | A;\ntype A = string;"))
}
