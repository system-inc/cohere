package typescript

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

const noMeaninglessVoidOperatorFile = "/repository/source/Discarding.ts"

func noMeaninglessVoidOperatorCaseName(index int) string {
	return "case" + strconv.Itoa(index)
}

// noMeaninglessVoidOperatorFinding is one expected finding with every layer it can be wrong at.
//
// The repair fields are mutually exclusive by branch, and that is the point of keeping both: this
// rule reports the SAME edit as a fix in one branch and a suggestion in the other, so a row asserts
// which one arrived as much as what it writes. An empty wantFixed means the row must carry no fix.
type noMeaninglessVoidOperatorFinding struct {
	wantSpan                  string
	wantMessage               string
	wantFixed                 string
	wantSuggestion            string
	wantSuggestionDescription string
}

// noMeaninglessVoidOperatorOptionsFor routes a case's options through the rule's own decoder.
func noMeaninglessVoidOperatorOptionsFor(t *testing.T, optionsJson string) any {
	t.Helper()
	if optionsJson == "" {
		return DefaultNoMeaninglessVoidOperatorSettings()
	}
	decoded, err := DecodeNoMeaninglessVoidOperatorOptions(json.RawMessage(optionsJson))
	if err != nil {
		t.Fatalf("decoding %s: %v", optionsJson, err)
	}
	return decoded
}

// applyNoMeaninglessVoidOperatorSuggestion rewrites source with one suggestion's fixes.
//
// The harness applies fixes and has no suggestion support, so the checkNever branch's repair would
// otherwise go entirely unasserted. That branch is exactly the one where a mistake matters most,
// since it is the half upstream refuses to apply unattended.
func applyNoMeaninglessVoidOperatorSuggestion(t *testing.T, source string, suggestion rule.Suggestion) string {
	t.Helper()
	fixes := append([]rule.Fix(nil), suggestion.Fixes...)
	sort.Slice(fixes, func(first, second int) bool { return fixes[first].Range.Pos() > fixes[second].Range.Pos() })
	for _, fix := range fixes {
		start, end := fix.Range.Pos(), fix.Range.End()
		if start < 0 || end > len(source) || start > end {
			t.Fatalf("the suggestion proposes an out-of-range edit [%d,%d) over %d bytes", start, end, len(source))
		}
		source = source[:start] + fix.Text + source[end:]
	}
	return source
}

// TestNoMeaninglessVoidOperatorStaysSilent is the clean corpus plus the measured clean divergences.
//
// Upstream ships only TWO passing cases, which is thin enough that it cannot see most of what this
// rule decides. The rest of the rows were measured against the installed 8.x build over a real
// program, and they pin the boundary of the type test in both directions: a number, `any`,
// `unknown`, a Promise of void, a union with a non-void member, and `never` with the option off.
func TestNoMeaninglessVoidOperatorStaysSilent(t *testing.T) {
	cases := []struct {
		sourceText  string
		optionsJson string
	}{
		{
			sourceText:  "\n(() => {})();\n\nfunction foo() {}\nfoo(); // nothing to discard\n\nfunction bar(x: number) {\n  void x;\n  return 2;\n}\nvoid bar(); // discarding a number\n    ",
			optionsJson: "",
		},
		{
			sourceText:  "\nfunction bar(x: never) {\n  void x;\n}\n    ",
			optionsJson: "",
		},
		{
			sourceText:  "const a = void 0;",
			optionsJson: "",
		},
		{
			sourceText:  "declare const x: void | number;\nvoid x;",
			optionsJson: "",
		},
		{
			sourceText:  "declare const x: never;\nvoid x;",
			optionsJson: "",
		},
		{
			sourceText:  "declare const x: any;\nvoid x;",
			optionsJson: "",
		},
		{
			sourceText:  "declare const x: unknown;\nvoid x;",
			optionsJson: "",
		},
		{
			sourceText:  "declare const p: Promise<void>;\nvoid p;",
			optionsJson: "",
		},
	}
	for index, testCase := range cases {
		t.Run(noMeaninglessVoidOperatorCaseName(index), func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, NoMeaninglessVoidOperator,
				noMeaninglessVoidOperatorFile, testCase.sourceText,
				noMeaninglessVoidOperatorOptionsFor(t, testCase.optionsJson)))
		})
	}
}

// TestNoMeaninglessVoidOperatorFires is the reporting corpus plus the measured divergences.
//
// Every row asserts the span, the whole rendered message, and the repair. The message matters more
// here than on most rules: it interpolates `typeToString`, so one rule renders "on void", "on
// undefined", "on never" and "on void | undefined" for different inputs, and upstream's corpus
// asserts the interpolation on NONE of its five cases.
//
// The repair assertions carry the branch. A row with wantFixed must arrive as a fix and must not
// carry a suggestion; a row with wantSuggestion must arrive as a suggestion and must not carry a
// fix. Collapsing the two would satisfy every message id while making the edit engine rewrite code
// upstream deliberately leaves to a person.
//
// The harness writes each fixture as `strings.TrimSpace(source)+"\n"`, so both the span slices and
// the expected rewrites are against that trimmed text rather than the Go literal.
func TestNoMeaninglessVoidOperatorFires(t *testing.T) {
	cases := []struct {
		sourceText      string
		optionsJson     string
		wantComposedFix string
		wantFindings    []noMeaninglessVoidOperatorFinding
	}{
		{
			sourceText:      "void (() => {})();",
			wantComposedFix: "(() => {})();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void (() => {})()",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "(() => {})();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "\nfunction foo() {}\nvoid foo();\n      ",
			wantComposedFix: "function foo() {}\nfoo();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void foo()",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\nfoo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "\nfunction bar(x: never) {\n  void x;\n}\n      ",
			wantComposedFix: "",
			optionsJson:     "{\"checkNever\": true}",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void x",
					wantMessage:               "void operator shouldn't be used on never; it should convey that a return value is being ignored",
					wantFixed:                 "",
					wantSuggestion:            "function bar(x: never) {\n  x;\n}\n",
					wantSuggestionDescription: "Remove 'void'",
				},
			},
		},
		{
			sourceText:      "function foo() {}\nvoid  foo();",
			wantComposedFix: "function foo() {}\nfoo();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void  foo()",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\nfoo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "function foo() {}\nvoid /* c */ foo();",
			wantComposedFix: "function foo() {}\nfoo();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void /* c */ foo()",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\nfoo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "function foo() {}\nvoid(foo());",
			wantComposedFix: "function foo() {}\n(foo());\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void(foo())",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\n(foo());\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "function foo(): undefined { return undefined; }\nvoid foo();",
			wantComposedFix: "function foo(): undefined { return undefined; }\nfoo();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void foo()",
					wantMessage:               "void operator shouldn't be used on undefined; it should convey that a return value is being ignored",
					wantFixed:                 "function foo(): undefined { return undefined; }\nfoo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "declare const x: void | undefined;\nvoid x;",
			wantComposedFix: "declare const x: void | undefined;\nx;\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void x",
					wantMessage:               "void operator shouldn't be used on void | undefined; it should convey that a return value is being ignored",
					wantFixed:                 "declare const x: void | undefined;\nx;\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:  "declare const x: never;\nvoid x;",
			optionsJson: "{\"checkNever\": true}",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void x",
					wantMessage:               "void operator shouldn't be used on never; it should convey that a return value is being ignored",
					wantFixed:                 "",
					wantSuggestion:            "declare const x: never;\nx;\n",
					wantSuggestionDescription: "Remove 'void'",
				},
			},
		},
		{
			sourceText:      "declare const x: void | never;\nvoid x;",
			wantComposedFix: "declare const x: void | never;\nx;\n",
			optionsJson:     "{\"checkNever\": true}",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void x",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "declare const x: void | never;\nx;\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
		{
			sourceText:      "function foo() {}\nvoid void foo();",
			wantComposedFix: "function foo() {}\nfoo();\n",
			optionsJson:     "",
			wantFindings: []noMeaninglessVoidOperatorFinding{
				{
					wantSpan:                  "void void foo()",
					wantMessage:               "void operator shouldn't be used on undefined; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\nvoid foo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
				{
					wantSpan:                  "void foo()",
					wantMessage:               "void operator shouldn't be used on void; it should convey that a return value is being ignored",
					wantFixed:                 "function foo() {}\nvoid foo();\n",
					wantSuggestion:            "",
					wantSuggestionDescription: "",
				},
			},
		},
	}
	for index, testCase := range cases {
		t.Run(noMeaninglessVoidOperatorCaseName(index), func(t *testing.T) {
			result := rule_testing.RunTypedWithOptions(t, NoMeaninglessVoidOperator,
				noMeaninglessVoidOperatorFile, testCase.sourceText,
				noMeaninglessVoidOperatorOptionsFor(t, testCase.optionsJson))

			wantIds := make([]string, len(testCase.wantFindings))
			for position := range wantIds {
				wantIds[position] = "meaninglessVoidOperator"
			}
			rule_testing.ExpectFindings(t, result, wantIds...)

			onDisk := strings.TrimSpace(testCase.sourceText) + "\n"
			for position, want := range testCase.wantFindings {
				diagnostic := result.Diagnostics[position]

				gotSpan := onDisk[diagnostic.Range.Pos():diagnostic.Range.End()]
				if gotSpan != want.wantSpan {
					t.Fatalf("finding %d span: expected %q, got %q", position, want.wantSpan, gotSpan)
				}
				if diagnostic.Message.Description != want.wantMessage {
					t.Fatalf("finding %d message: expected %q, got %q", position, want.wantMessage, diagnostic.Message.Description)
				}

				if want.wantFixed != "" {
					if len(diagnostic.Suggestions) != 0 {
						t.Fatalf("finding %d: the fix branch must offer no suggestion, got %d",
							position, len(diagnostic.Suggestions))
					}
					if len(diagnostic.Fixes) != 1 {
						t.Fatalf("finding %d: expected one fix, got %d", position, len(diagnostic.Fixes))
					}
					continue
				}

				if len(diagnostic.Fixes) != 0 {
					t.Fatalf("finding %d: the suggestion branch must apply nothing unattended, got %d fixes",
						position, len(diagnostic.Fixes))
				}
				if len(diagnostic.Suggestions) != 1 {
					t.Fatalf("finding %d: expected one suggestion, got %d", position, len(diagnostic.Suggestions))
				}
				suggestion := diagnostic.Suggestions[0]
				if suggestion.Message.Description != want.wantSuggestionDescription {
					t.Fatalf("finding %d suggestion text: expected %q, got %q",
						position, want.wantSuggestionDescription, suggestion.Message.Description)
				}
				if suggestion.Message.Id != "removeVoid" {
					t.Fatalf("finding %d suggestion id: got %q", position, suggestion.Message.Id)
				}
				applied := applyNoMeaninglessVoidOperatorSuggestion(t, onDisk, suggestion)
				if applied != want.wantSuggestion {
					t.Fatalf("finding %d suggestion applied: expected %q, got %q",
						position, want.wantSuggestion, applied)
				}
			}

			// The whole-file rewrite, for every row whose findings all carry fixes. This is the
			// assertion a message-id fixture cannot make: a fix removing the right span with the
			// wrong text passes everything above.
			// The whole-file rewrite, asserted against a literal measured from the installed
			// build rather than against anything this rule produced.
			//
			// `wantComposedFix` is the result of applying EVERY fix on the row, which for the
			// double-void case is not the same as either finding's own rewrite. Comparing against
			// one finding's isolated rewrite asserted a file the pipeline never produces, and
			// comparing against a value recomputed from the rule's own output would have asserted
			// nothing at all.
			if testCase.wantComposedFix != "" {
				rule_testing.ExpectFixedSource(t, result, testCase.wantComposedFix)
			}
		})
	}
}

// TestDecodeNoMeaninglessVoidOperatorOptions pins the decoder against its default.
//
// This option defaults to FALSE, so an absent key and a zero value happen to agree and the generic
// decoder would work by luck. The decoder is hand-rolled anyway and tested anyway, because the
// coincidence is not a property of the option surface: adding one default-true key would make the
// generic path silently wrong, and this test is what would notice.
func TestDecodeNoMeaninglessVoidOperatorOptions(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "emptyObject", raw: `{}`, want: false},
		{name: "explicitTrue", raw: `{"checkNever": true}`, want: true},
		{name: "explicitFalse", raw: `{"checkNever": false}`, want: false},
		{name: "unrelatedKeyOnly", raw: `{"somethingElse": 1}`, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := DecodeNoMeaninglessVoidOperatorOptions(json.RawMessage(testCase.raw))
			if err != nil {
				t.Fatalf("decoding %s: %v", testCase.raw, err)
			}
			options, isOptions := decoded.(NoMeaninglessVoidOperatorOptions)
			if !isOptions {
				t.Fatalf("the decoder returned %T rather than the options struct", decoded)
			}
			if options.CheckNever != testCase.want {
				t.Fatalf("checkNever: expected %v, got %v", testCase.want, options.CheckNever)
			}
		})
	}
}

// TestNoMeaninglessVoidOperatorFallsBackToTheDefaultOnNilOptions pins the nil-options path.
//
// A rule configured as a bare "error" is handed nil options, and every fixture above reaches the
// rule through the decoder, so nothing there can see the fallback. The separating input is a
// `never` argument: silent under the default, reporting when checkNever is on.
func TestNoMeaninglessVoidOperatorFallsBackToTheDefaultOnNilOptions(t *testing.T) {
	const neverArgument = "declare const x: never;\nvoid x;"

	rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, NoMeaninglessVoidOperator,
		noMeaninglessVoidOperatorFile, neverArgument, nil))

	// The control: the same input with checkNever on must report, so the silence above is the
	// default being applied rather than the rule being inert.
	reported := rule_testing.RunTypedWithOptions(t, NoMeaninglessVoidOperator,
		noMeaninglessVoidOperatorFile, neverArgument,
		NoMeaninglessVoidOperatorOptions{CheckNever: true})
	rule_testing.ExpectFindings(t, reported, "meaninglessVoidOperator")
}

// TestNoMeaninglessVoidOperatorNeedsTheTypedHarness pins the checker declaration.
//
// Under the plain harness the checker is nil, this rule returns immediately, and every clean
// fixture would pass having proven nothing. Asserting the declaration means a later revert fails
// loudly rather than going vacuously green.
func TestNoMeaninglessVoidOperatorNeedsTheTypedHarness(t *testing.T) {
	if !NoMeaninglessVoidOperator.NeedsTypeChecker {
		t.Fatal("the rule resolves the argument's type, so it must declare NeedsTypeChecker")
	}

	rule_testing.ExpectClean(t, rule_testing.Run(t, NoMeaninglessVoidOperator,
		noMeaninglessVoidOperatorFile, "function foo() {}\nvoid foo();"))
}
