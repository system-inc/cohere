package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// runLogicalAssignment runs the rule with a case's options, defaulting to upstream's default when
// the case names none.
//
// RunTyped rather than Run, because the rule declares NeedsTypeChecker and the plain harness hands
// it a nil checker. Six of upstream's clean cases shadow `undefined` or `Boolean` and report
// without one, so running these untyped would fail six cases for a reason that has nothing to do
// with the rule.
//
// RunTyped trims the fixture to `strings.TrimSpace(contents)+"\n"` before writing it, so every
// expectation compared against the whole file has to be transformed the same way. That is what
// logicalAssignmentAsWritten does, and it is the harness fact the brief records a porter chasing
// through the intermediate representation and the shim before finding.
func runLogicalAssignment(test *testing.T, testCase struct {
	source  string
	options any
},
) rule_testing.Result {
	test.Helper()
	options := testCase.options
	if options == nil {
		options = DefaultLogicalAssignmentOperatorsSettings()
	}
	if logicalAssignmentNeedsScriptSource(testCase.source) {
		return rule_testing.RunWithOptions(test, LogicalAssignmentOperators, "logical.ts",
			testCase.source, options)
	}
	return rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		testCase.source, options)
}

// logicalAssignmentNeedsScriptSource picks the untyped harness for the `with` cases, and the reason
// is a fact about the harness rather than about the rule.
//
// `rule_testing`'s tsconfig pins `moduleDetection: "force"` (internal/rule_testing/program.go:27),
// which makes every typed fixture an external module no matter what it contains. A module is strict
// by specification, and `with` is illegal in strict code, so under RunTyped the rule's `isStrict`
// is unconditionally true and the non-strict `with` branch is unreachable through that harness.
// Upstream runs its whole corpus at `sourceType: "script"`, where those seven cases are the only
// thing exercising that branch.
//
// The untyped harness parses the text without forcing moduleness, so it reproduces upstream's
// source type for exactly these cases. What it costs is the checker, which those cases do not need:
// none of them shadows `undefined` or `Boolean`, so the nil-checker fallback gives the same answer a
// real checker would. The two harness limitations are disjoint, which is the only reason splitting
// on the source works at all.
//
// Pinned by TestLogicalAssignmentOperatorsWithBlocksNeedAScriptSource below, with controls, so this
// split is a measured claim rather than a convenience.
func logicalAssignmentNeedsScriptSource(source string) bool {
	return strings.Contains(source, "with (")
}

// logicalAssignmentAsWritten transforms an expectation the way the harness transformed its input.
//
// The two harnesses differ here and the difference is invisible until a whole-file comparison runs.
// `RunTyped` writes `strings.TrimSpace(contents)+"\n"` to disk (internal/rule_testing/program.go),
// so every expectation compared against the rewritten file needs the same trim and terminator.
// `Run` parses the string as given and does not trim, so applying the transform there would add a
// newline the file does not have.
//
// Keyed on the same predicate that picks the harness, so the two cannot drift: a case routed to the
// untyped harness is compared untrimmed, and one routed to the typed harness is compared trimmed.
// Padding the rule to make either comparison line up would be the wrong repair, since the
// difference is in the harness rather than in the fixer.
func logicalAssignmentAsWritten(original string, expected string) string {
	if logicalAssignmentNeedsScriptSource(original) {
		return expected
	}
	return strings.TrimSpace(expected) + "\n"
}

// TestLogicalAssignmentOperatorsStaysSilent runs upstream's whole valid list.
//
// These are the false positives upstream already thought about, and they are the half that catches
// a rule which fires on correct code. Several of them separate distinctions this port could
// otherwise get away with: `a = (a || b)` is clean because the right side is parenthesized, and
// `a = (a || b) || c` is clean because the leftmost walk stops at an explicit grouping.
func TestLogicalAssignmentOperatorsStaysSilent(test *testing.T) {
	test.Parallel()

	for _, testCase := range logicalAssignmentCleanCases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestLogicalAssignmentOperatorsFires runs upstream's whole invalid list and asserts the message id
// of each finding.
func TestLogicalAssignmentOperatorsFires(test *testing.T) {
	test.Parallel()

	for _, testCase := range logicalAssignmentReportingCases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			rule_testing.ExpectFindings(test, result, testCase.messageId)
		})
	}
}

// TestLogicalAssignmentOperatorsFixes applies every repair upstream asserts and compares the whole
// rewritten file.
//
// This is the half no message-id fixture can see. Upstream carries 142 `output` values against 184
// cases, and each is the exact text the fixer must produce, so a repair landing on the right span
// with the wrong text fails here rather than passing.
func TestLogicalAssignmentOperatorsFixes(test *testing.T) {
	test.Parallel()

	checked := 0
	for _, testCase := range logicalAssignmentReportingCases {
		if testCase.fixedSource == "" {
			continue
		}
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			rule_testing.ExpectFixedSource(test, result,
				logicalAssignmentAsWritten(testCase.source, testCase.fixedSource))
		})
		checked++
	}
	// Pinned so a regeneration that silently drops the output field fails here rather than passing
	// vacuously with nothing to check.
	if checked != 142 {
		test.Errorf("checked %d fixes, want 142", checked)
	}
}

// TestLogicalAssignmentOperatorsSuggestions applies every suggestion upstream asserts.
//
// `rule_testing` can apply a fix but not a suggestion, so the applier here is hand-rolled. It is
// the same replay `ExpectFixedSource` performs: fixes back to front so an earlier replacement
// cannot move the offsets a later one was computed against.
//
// The 28 cases here are the getter judgment made visible. Every one of them is a member access
// rather than a bare identifier, and upstream refuses to rewrite them unattended because reading
// `a.b` once where the source read it twice can skip a getter call.
func TestLogicalAssignmentOperatorsSuggestions(test *testing.T) {
	test.Parallel()

	checked := 0
	for _, testCase := range logicalAssignmentReportingCases {
		if len(testCase.suggestions) == 0 {
			continue
		}
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d", len(result.Diagnostics))
			}
			offered := result.Diagnostics[0].Suggestions
			if len(offered) != len(testCase.suggestions) {
				test.Fatalf("offered %d suggestions, want %d", len(offered), len(testCase.suggestions))
			}
			// A suggestion must never arrive alongside a fix: the two report calls are exclusive
			// upstream, and a rule offering both would have the edit engine apply the repair a
			// human was meant to choose.
			if len(result.Diagnostics[0].Fixes) != 0 {
				test.Fatalf("a suggestion case also carried %d fixes, which would be applied unattended",
					len(result.Diagnostics[0].Fixes))
			}
			for index, want := range testCase.suggestions {
				if offered[index].Message.Id != want.id {
					test.Errorf("suggestion %d is %q, want %q", index, offered[index].Message.Id, want.id)
					continue
				}
				applied := applyLogicalAssignmentFixes(result.SourceFile.Text(), offered[index].Fixes)
				if applied != logicalAssignmentAsWritten(testCase.source, want.output) {
					test.Errorf("applying %q produced\n  %q\nwant\n  %q",
						want.id, applied, want.output)
				}
			}
		})
		checked += len(testCase.suggestions)
	}
	if checked != 28 {
		test.Errorf("checked %d suggestions, want 28", checked)
	}
}

// applyLogicalAssignmentFixes replays a repair into source text, back to front.
//
// Back to front for the same reason `ExpectFixedSource` does it: an earlier fix's replacement would
// otherwise move the offsets every later one was computed against. Insertions at a shared offset
// keep their relative order, which matters for the second shape, where an opening parenthesis and
// a deletion can start at the same position.
func applyLogicalAssignmentFixes(source string, fixes []rule.Fix) string {
	ordered := make([]rule.Fix, len(fixes))
	copy(ordered, fixes)
	for outer := 1; outer < len(ordered); outer++ {
		current := ordered[outer]
		inner := outer - 1
		for inner >= 0 && ordered[inner].Range.Pos() < current.Range.Pos() {
			ordered[inner+1] = ordered[inner]
			inner--
		}
		ordered[inner+1] = current
	}
	fixed := source
	for _, fix := range ordered {
		fixed = fixed[:fix.Range.Pos()] + fix.Text + fixed[fix.Range.End():]
	}
	return fixed
}

// TestLogicalAssignmentOperatorsDeclinesToRepair pins the cases upstream reports and deliberately
// leaves alone, which is a decision rather than an omission.
//
// Upstream writes them as `output: null` with an empty `suggestions` array, meaning the fixer ran
// and yielded nothing. Three are comments inside the rewritten span, and the rest are the automatic
// semicolon insertion hazard in the `if` shape.
//
// A fixer that repaired one of these would pass every message-id fixture while writing source
// upstream refuses to write, so the absence is asserted directly.
func TestLogicalAssignmentOperatorsDeclinesToRepair(test *testing.T) {
	test.Parallel()

	declined := 0
	for _, testCase := range logicalAssignmentReportingCases {
		if testCase.fixedSource != "" || len(testCase.suggestions) != 0 {
			continue
		}
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d", len(result.Diagnostics))
			}
			if count := len(result.Diagnostics[0].Fixes); count != 0 {
				test.Errorf("proposed %d fixes on a case upstream refuses to repair", count)
			}
			if count := len(result.Diagnostics[0].Suggestions); count != 0 {
				test.Errorf("offered %d suggestions on a case upstream refuses to repair", count)
			}
		})
		declined++
	}
	if declined != 14 {
		test.Errorf("checked %d declined repairs, want 14", declined)
	}
}

// TestLogicalAssignmentOperatorsSplitsFixesFromSuggestions asserts the getter judgment directly,
// rather than only through the outputs it produces.
//
// The distinction is what the edit engine acts on: a fix is applied with nobody watching and a
// suggestion is not. A rule that offered everything as a fix would rewrite getters across the tree
// and every one of its message-id fixtures would stay green, which is why this is asserted as its
// own property.
func TestLogicalAssignmentOperatorsSplitsFixesFromSuggestions(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		source      string
		options     any
		wantFix     bool
		wantSuggest bool
	}{
		{"a bare identifier is safe to read once", "a = a || b", nil, true, false},
		{"a member access may be a getter", "a.b = a.b || c", nil, false, true},
		{"a single property off an identifier is fixed in the logical shape",
			"a.b || (a.b = c)", nil, true, false},
		{"a deeper access is only suggested", "a.b.c || (a.b.c = d)", nil, false, true},
		{"an identifier in a with block may be a scrutinee property",
			"with (object) a = a || b", nil, false, true},
		{"a strict file turns the with allowance back off",
			"\"use strict\";\nwith (object) a = a || b", nil, true, false},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d: %v", len(result.Diagnostics),
					result.MessageIds())
			}
			gotFix := len(result.Diagnostics[0].Fixes) > 0
			gotSuggest := len(result.Diagnostics[0].Suggestions) > 0
			if gotFix != testCase.wantFix {
				test.Errorf("carried a fix: %v, want %v", gotFix, testCase.wantFix)
			}
			if gotSuggest != testCase.wantSuggest {
				test.Errorf("offered a suggestion: %v, want %v", gotSuggest, testCase.wantSuggest)
			}
		})
	}
}

// TestLogicalAssignmentOperatorsSpans asserts where each finding points.
//
// `ExpectFindings` asserts ids and count and nothing else, so a rule reporting the right judgment
// at the wrong node passes a complete fixture pair while being wrong. That matters twice over here
// because three of the four report sites carry a repair, and a finding shown at one place while its
// edit lands at another is the shape the report API's own doc comment warns about.
//
// Upstream reports the whole assignment, the whole logical expression, and the whole `if` statement
// respectively, rather than the operator.
func TestLogicalAssignmentOperatorsSpans(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name     string
		source   string
		options  any
		wantSpan string
	}{
		{"the assignment shape points at the whole assignment", "a = a || b", nil, "a = a || b"},
		{"the logical shape points at the whole logical expression", "a || (a = b)", nil,
			"a || (a = b)"},
		{"the if shape points at the whole if statement", "if (a) a = b",
			logicalAssignmentAlwaysIfOptions(), "if (a) a = b"},
		{"the never shape points at the whole assignment", "a ||= b",
			logicalAssignmentNeverOptions(), "a ||= b"},
		{"a nested assignment reports only its own span", "x = (a = a || b)", nil, "a = a || b"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d: %v", len(result.Diagnostics),
					result.MessageIds())
			}
			diagnostic := result.Diagnostics[0]
			got := result.SourceFile.Text()[diagnostic.Range.Pos():diagnostic.Range.End()]
			if got != testCase.wantSpan {
				test.Errorf("finding pointed at %q, want %q", got, testCase.wantSpan)
			}
		})
	}
}

// TestLogicalAssignmentOperatorsMessages asserts the rendered message text.
//
// Every message here interpolates the operator, so the id assertion cannot see anything the format
// string does. A mutation moving only the per-finding text would leave the count and the id fixed
// and read as a surviving mutant with no visible cause, which the brief records happening twice to
// one porter.
//
// Compared against literal strings typed here rather than against the rule's own message
// constructors, because comparing a diagnostic to the very function it was built by is an equality
// whose two sides move together under mutation.
func TestLogicalAssignmentOperatorsMessages(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source     string
		options    any
		wantId     string
		wantPrefix string
	}{
		{"a = a || b", nil, "assignment",
			"Assignment (=) can be replaced with operator assignment (||=)."},
		{"a = a ?? b", nil, "assignment",
			"Assignment (=) can be replaced with operator assignment (??=)."},
		{"a || (a = b)", nil, "logical",
			"Logical expression can be replaced with an assignment (||=)."},
		{"if (a) a = b", logicalAssignmentAlwaysIfOptions(), "if",
			"'if' statement can be replaced with a logical operator assignment with operator &&="},
		{"if (a == null) a = b", logicalAssignmentAlwaysIfOptions(), "if",
			"'if' statement can be replaced with a logical operator assignment with operator ??="},
		{"a &&= b", logicalAssignmentNeverOptions(), "unexpected",
			"Unexpected logical operator assignment (&&=) shorthand."},
	}
	for _, testCase := range cases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d", len(result.Diagnostics))
			}
			message := result.Diagnostics[0].Message
			if message.Id != testCase.wantId {
				test.Errorf("message id is %q, want %q", message.Id, testCase.wantId)
			}
			if !strings.HasPrefix(message.Description, testCase.wantPrefix) {
				test.Errorf("message description is\n  %q\nwant it to start with\n  %q",
					message.Description, testCase.wantPrefix)
			}
		})
	}
}

// TestLogicalAssignmentOperatorsSuggestionMessages asserts the four suggestion descriptions, which
// no other test reaches.
//
// The suggestion test above asserts ids and applied text; a wrong description would pass both while
// telling a human choosing the repair the wrong thing.
func TestLogicalAssignmentOperatorsSuggestionMessages(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source  string
		options any
		wantId  string
		want    string
	}{
		{"a.b = a.b || c", nil, "useLogicalOperator",
			"Convert this assignment to use the operator ||=."},
		{"a.b.c || (a.b.c = d)", nil, "convertLogical",
			"Replace this logical expression with an assignment with the operator ||=."},
		{"if (a.b.c) a.b.c = d", logicalAssignmentAlwaysIfOptions(), "convertIf",
			"Replace this 'if' statement with a logical assignment with operator &&=."},
		{"a.b ||= c", logicalAssignmentNeverOptions(), "separate",
			"Separate the logical assignment into an assignment with a logical operator."},
	}
	for _, testCase := range cases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			if len(result.Diagnostics) != 1 || len(result.Diagnostics[0].Suggestions) != 1 {
				test.Fatalf("wanted one finding with one suggestion, got %d findings",
					len(result.Diagnostics))
			}
			suggestion := result.Diagnostics[0].Suggestions[0]
			if suggestion.Message.Id != testCase.wantId {
				test.Errorf("suggestion id is %q, want %q", suggestion.Message.Id, testCase.wantId)
			}
			if suggestion.Message.Description != testCase.want {
				test.Errorf("suggestion description is\n  %q\nwant\n  %q",
					suggestion.Message.Description, testCase.want)
			}
		})
	}
}

// TestDecodeLogicalAssignmentOperatorsOptions puts the decoder under test directly.
//
// The wire shape is upstream's own option list: the rule registers with `DecodeOptionList`, so the
// config layer hands over every element after the severity rather than the single object
// `rule.DecodeOptionsInto` would decode. The default is also not the zero value: an absent
// option means `always`, while the zero value of the setting is the empty string, which matches
// neither arm and would make the rule silent on every file it exists to catch.
func TestDecodeLogicalAssignmentOperatorsOptions(test *testing.T) {
	test.Parallel()

	always := LogicalAssignmentAlways
	never := LogicalAssignmentNever

	cases := []struct {
		name      string
		raw       string
		wantMode  *LogicalAssignmentSetting
		wantIf    bool
		wantError bool
	}{
		{"absent options default to always", "", &always, false, false},
		{"an empty array defaults to always", `[]`, &always, false, false},
		{"an explicit always", `["always"]`, &always, false, false},
		{"always with the if check on", `["always", {"enforceForIfStatements": true}]`,
			&always, true, false},
		{"always with the if check explicitly off",
			`["always", {"enforceForIfStatements": false}]`, &always, false, false},
		{"always with an empty object leaves the if check off", `["always", {}]`,
			&always, false, false},
		{"never", `["never"]`, &never, false, false},
		{"a bare string, which the config layer never delivers to a list rule, is refused",
			`"never"`, nil, false, true},
		{"an unknown key in the second element is refused",
			`["always", {"enforceForIfStatement": true}]`, nil, false, true},
		{"a third element is refused", `["always", {}, "never"]`, nil, false, true},
		{"an unknown mode is an error rather than a silent default", `["sometimes"]`,
			nil, false, true},
		{"options beside never are refused", `["never", {"enforceForIfStatements": true}]`,
			nil, false, true},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeLogicalAssignmentOperatorsOptionsError := DecodeLogicalAssignmentOperatorsOptions(
				[]byte(testCase.raw),
			)
			if testCase.wantError {
				if decodeLogicalAssignmentOperatorsOptionsError == nil {
					test.Fatalf("wanted an error, got %+v", decoded)
				}
				return
			}
			if decodeLogicalAssignmentOperatorsOptionsError != nil {
				test.Fatalf("unexpected error: %v", decodeLogicalAssignmentOperatorsOptionsError)
			}
			settings, ok := decoded.(LogicalAssignmentOperatorsOptions)
			if !ok {
				test.Fatalf("decoded options are not LogicalAssignmentOperatorsOptions: got %T", decoded)
			}
			if settings.Require == nil || *settings.Require != *testCase.wantMode {
				test.Errorf("mode is %v, want %v", settings.Require, *testCase.wantMode)
			}
			if settings.EnforceForIfStatements != testCase.wantIf {
				test.Errorf("enforceForIfStatements is %v, want %v",
					settings.EnforceForIfStatements, testCase.wantIf)
			}
		})
	}
}

// TestLogicalAssignmentOperatorsHandlesNilOptions is the inert-rule guard.
//
// A rule configured as a bare severity is handed nil options, and `options.(T)` on nil yields the
// zero value: a nil Require, which matches neither mode. The rule falls back to upstream's default
// there, and this asserts it does. The brief records a rule that registered on 3,407 files and was
// completely broken this way, with every fixture passing because every fixture reached the rule
// through the decoder.
func TestLogicalAssignmentOperatorsHandlesNilOptions(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunTyped(test, LogicalAssignmentOperators, "logical.ts", "a = a || b")
	rule_testing.ExpectFindings(test, result, "assignment")
	rule_testing.ExpectFixedSource(test, result, logicalAssignmentAsWritten("a = a || b", "a ||= b"))

	// The control: the if-statement arm must stay off under the default, or the fallback would be
	// enforcing something nobody configured.
	silent := rule_testing.RunTyped(test, LogicalAssignmentOperators, "logical.ts", "if (a) a = b")
	rule_testing.ExpectClean(test, silent)
}

// TestLogicalAssignmentOperatorsUnwrapsParenthesesInTheLogicalShape is the parser divergence, pinned
// as its own test because no imported fixture can fail for the right reason.
//
// Upstream's parser folds parentheses away, so its selector matches the assignment directly. Ours
// keeps a KindParenthesizedExpression, and without unwrapping it this arm finds nothing at all
// while every clean case still passes. That is a silent loss of all 37 of upstream's cases for the
// shape, and the fixture pair alone cannot distinguish it from a rule with nothing to report.
//
// Measured in internal/logical_assignment_operators_probe: the right side of `a || (a = b)` is
// KindParenthesizedExpression wrapping a KindBinaryExpression.
func TestLogicalAssignmentOperatorsUnwrapsParenthesesInTheLogicalShape(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		"a || (a = b)", DefaultLogicalAssignmentOperatorsSettings())
	rule_testing.ExpectFindings(test, result, "logical")

	// The doubly-parenthesized form nests, which is why the unwrap is a loop rather than one step.
	nested := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		"a || ((a = b))", DefaultLogicalAssignmentOperatorsSettings())
	rule_testing.ExpectFindings(test, nested, "logical")
}

// TestLogicalAssignmentOperatorsTypeScriptShapes covers syntax upstream's corpus cannot express
// except through a fixture parser, and syntax it cannot express at all.
//
// The gap between upstream's corpus and this tree is exactly TypeScript, and the brief is explicit
// that closing it is the porter's job rather than something the imported cases can do. Four of
// these are upstream's own fixture-parser cases, which arrive here as ordinary inputs; the rest are
// shapes upstream has no way to write.
//
// The non-null assertion cases are the ones that matter most, because they sit between the target
// and the operator, which is exactly where the brief records two fixers losing type information.
func TestLogicalAssignmentOperatorsTypeScriptShapes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name      string
		source    string
		options   any
		wantIds   []string
		wantFixed string
	}{
		{
			name:      "an assertion on the right survives the assignment repair",
			source:    "a = a || (b as number)",
			wantIds:   []string{"assignment"},
			wantFixed: "a ||= (b as number)",
		},
		{
			name:      "a bare assertion on the right survives too",
			source:    "a = a || b as number",
			wantIds:   []string{"assignment"},
			wantFixed: "a ||= b as number",
		},
		{
			// A non-null assertion is not a valid assignment target, so the shape this was first
			// written as (`a! = a! || b`) does not parse as an assignment at all. It survives on
			// the right instead, which is where the repair copies text.
			name:      "a non-null assertion on the right survives the assignment repair",
			source:    "a = a || b!",
			wantIds:   []string{"assignment"},
			wantFixed: "a ||= b!",
		},
		{
			name:      "a non-null assertion inside the target is carried through, not dropped",
			source:    "a!.b = a!.b || c",
			wantIds:   []string{"assignment"},
			wantFixed: "",
		},
		{
			name:      "a satisfies expression on the right survives",
			source:    "a = a || (b satisfies number)",
			wantIds:   []string{"assignment"},
			wantFixed: "a ||= (b satisfies number)",
		},
		{
			name:      "the never expansion copies an assertion on the target rather than rendering it",
			source:    "a!.b ||= c",
			options:   logicalAssignmentNeverOptions(),
			wantIds:   []string{"unexpected"},
			wantFixed: "",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, testCase.options})
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
			if testCase.wantFixed != "" {
				rule_testing.ExpectFixedSource(test, result,
					logicalAssignmentAsWritten(testCase.source, testCase.wantFixed))
			}
		})
	}
}

// TestLogicalAssignmentOperatorsNeverExpansionKeepsTheTargetText asserts that the `never` repair
// copies the target out of the source rather than re-rendering it.
//
// A fixer that rebuilt the target from the node would lose whatever it did not think to rebuild,
// which is the failure the brief records twice: a lost return annotation and a stranded type
// annotation, both from fixers that constructed a replacement instead of copying a span. Here the
// target is written a second time by the repair, so anything inside it is at risk.
func TestLogicalAssignmentOperatorsNeverExpansionKeepsTheTargetText(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source string
		want   string
	}{
		{"a ||= b", "a = a || b"},
		{"a!.b ||= c", "a!.b = a!.b || c"},
		{"a[b as number] ||= c", "a[b as number] = a[b as number] || c"},
		{"this.prop ??= 0", "this.prop = this.prop ?? 0"},
	}
	for _, testCase := range cases {
		test.Run(testCase.source, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
				testCase.source, logicalAssignmentNeverOptions())
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d", len(result.Diagnostics))
			}
			repairs := result.Diagnostics[0].Fixes
			if len(repairs) == 0 {
				repairs = result.Diagnostics[0].Suggestions[0].Fixes
			}
			applied := applyLogicalAssignmentFixes(result.SourceFile.Text(), repairs)
			if applied != logicalAssignmentAsWritten(testCase.source, testCase.want) {
				test.Errorf("expanded to\n  %q\nwant\n  %q", applied, testCase.want)
			}
		})
	}
}

// TestLogicalAssignmentOperatorsResolvesUndefinedThroughTheChecker covers the one judgment that
// consults name resolution.
//
// Upstream asks its scope analysis whether `undefined` is the global rather than a local shadowing
// it, because a local `undefined` holding some other value makes `a === undefined` an ordinary
// comparison rather than a nullish test. The same question is asked here as the complement, by
// asking whether the name is declared in this file, for the reason the brief gives: the real global
// has zero declarations, so asking whether it resolves to a global answers false on exactly the
// inputs the rule must accept.
//
// Run through the typed harness, because the untyped one hands the rule a nil checker and the guard
// then reads every `undefined` as the global. That fallback is the right answer for source that does
// not shadow it, which is why the untyped fixtures above agree.
func TestLogicalAssignmentOperatorsResolvesUndefinedThroughTheChecker(test *testing.T) {
	test.Parallel()

	reported := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "resolve.ts",
		"if (a === null || a === undefined) a = b;", logicalAssignmentAlwaysIfOptions())
	rule_testing.ExpectFindings(test, reported, "if")

	// A local `undefined` is a different value, so the pair is no longer a nullish test.
	shadowed := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "resolve.ts",
		"let undefined = 1;\nif (a === null || a === undefined) a = b;",
		logicalAssignmentAlwaysIfOptions())
	rule_testing.ExpectClean(test, shadowed)

	// The same for `Boolean`, whose cast arm resolves the callee the same way.
	cast := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "resolve.ts",
		"if (Boolean(a)) a = b;", logicalAssignmentAlwaysIfOptions())
	rule_testing.ExpectFindings(test, cast, "if")

	shadowedCast := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "resolve.ts",
		"function Boolean(value: unknown) { return 1; }\nif (Boolean(a)) a = b;",
		logicalAssignmentAlwaysIfOptions())
	rule_testing.ExpectClean(test, shadowedCast)
}

// TestLogicalAssignmentOperatorsWithBlocksNeedAScriptSource pins the harness split above, with the
// controls that make it a measurement rather than an assertion.
//
// The claim has two halves and each needs its own control, because a split that is wrong in either
// direction would leave seven imported cases passing for the wrong reason.
//
// First: under the typed harness a `with` block reads as strict, so the rule offers an unattended
// fix where upstream offers a suggestion. That is the harness forcing moduleness, not a rule defect,
// and the assertion here is that the difference is real and in the direction claimed.
//
// Second: the untyped harness reproduces upstream, and the checker those cases give up is one they
// never needed. The control for that is a shadowing case run untyped, which reports where it should
// be clean, showing the nil-checker fallback is a real limitation that simply does not reach here.
func TestLogicalAssignmentOperatorsWithBlocksNeedAScriptSource(test *testing.T) {
	test.Parallel()

	source := "with (object) a = a || b"

	// Untyped, which is upstream's script source type: the identifier may be a scrutinee property,
	// so the repair is offered rather than applied.
	untyped := rule_testing.RunWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		source, DefaultLogicalAssignmentOperatorsSettings())
	if len(untyped.Diagnostics) != 1 {
		test.Fatalf("untyped: wanted one finding, got %d", len(untyped.Diagnostics))
	}
	if len(untyped.Diagnostics[0].Suggestions) != 1 || len(untyped.Diagnostics[0].Fixes) != 0 {
		test.Errorf("untyped: wanted one suggestion and no fix, got %d suggestions and %d fixes",
			len(untyped.Diagnostics[0].Suggestions), len(untyped.Diagnostics[0].Fixes))
	}

	// Typed, where moduleDetection force makes the file a module and therefore strict. The rule
	// then treats the identifier as an ordinary variable and applies the repair. This is asserted
	// rather than avoided, so a later harness change that stops forcing moduleness fails here and
	// names the reason instead of silently making the split above pointless.
	typed := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		source, DefaultLogicalAssignmentOperatorsSettings())
	if len(typed.Diagnostics) != 1 {
		test.Fatalf("typed: wanted one finding, got %d", len(typed.Diagnostics))
	}
	if len(typed.Diagnostics[0].Fixes) == 0 {
		test.Errorf("typed: expected the forced module to read as strict and carry a fix, got %d fixes",
			len(typed.Diagnostics[0].Fixes))
	}

	// The control on the other limitation: a shadowed `undefined` needs the checker, and the
	// untyped harness cannot see it. This case is clean upstream and reports here, which is what
	// makes the split above load bearing rather than arbitrary. No `with` case shadows anything,
	// so the two limitations never overlap.
	shadowed := rule_testing.RunWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		"{\n    const undefined = 0;\n    if (a == undefined) a = b\n}",
		logicalAssignmentAlwaysIfOptions())
	if len(shadowed.Diagnostics) != 1 {
		test.Errorf("control: the untyped harness was expected to miss the shadow and report, got %d findings",
			len(shadowed.Diagnostics))
	}

	// And the same case typed, which is clean. Together these two say exactly what each harness can
	// and cannot see.
	resolved := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "logical.ts",
		"{\n    const undefined = 0;\n    if (a == undefined) a = b\n}",
		logicalAssignmentAlwaysIfOptions())
	rule_testing.ExpectClean(test, resolved)
}

// TestLogicalAssignmentOperatorsWrapsWhereUpstreamWraps covers the outer-parenthesis decision on
// shapes upstream's corpus never writes.
//
// The corpus contains exactly one case in this family, `fn(a || (a = 0))`, and it is the one shape
// that does not gain parentheses. A port reading that single case as evidence about call arguments
// generally, or about delimited positions generally, gets every other shape here wrong and stays
// green through the whole imported corpus. That is what happened: the first version of this rule
// carried a semantic helper about argument and element positions, all four rows below came out
// bare, and only a surviving mutant on that helper found it.
//
// The mechanism upstream actually uses is a token test, `astUtils.isParenthesised`, which asks
// whether the tokens immediately around the node are parentheses without asking whose they are. A
// sole argument borrows the call's own parentheses and a second argument does not, which is why the
// first two rows differ despite being the same kind of position.
//
// Every expectation here was measured by driving the installed ESLint rule, not derived from
// reading. Both readings of the source looked correct and one of them was wrong.
func TestLogicalAssignmentOperatorsWrapsWhereUpstreamWraps(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"a sole argument borrows the call's own parentheses", "fn(a || (a = 0))", "fn(a ||= 0)"},
		{"a second argument is preceded by a comma, so it wraps", "fn(1, a || (a = 0))",
			"fn(1, (a ||= 0))"},
		{"an array element wraps", "[a || (a = b)]", "[(a ||= b)]"},
		{"a property value wraps", "({x: a || (a = b)})", "({x: (a ||= b)})"},
		{"a variable initialiser wraps", "var q = a || (a = b)", "var q = (a ||= b)"},
		{"a sole argument to new borrows its parentheses too", "new Thing(a || (a = 0))",
			"new Thing(a ||= 0)"},
		// The two halves of the token test are separable, and this row is the only thing that
		// reaches the closing half. A first argument of several is preceded by the call's `(` and
		// followed by a comma, so the opening half accepts and the closing half must refuse. With
		// the closing half neutralized this row comes out bare, and every other row in this test
		// still passes, which is exactly how it read as a surviving mutant.
		{"a first argument of several is preceded by a paren but followed by a comma",
			"fn(a || (a = 0), 1)", "fn((a ||= 0), 1)"},
		{"an if condition's own parentheses surround a sole operand", "if (a || (a = b)) c;",
			"if (a ||= b) c;"},
		{"a for condition is delimited by semicolons and wraps", "for (;a || (a = b);) c;",
			"for (;(a ||= b);) c;"},
		{"an expression statement needs nothing", "a || (a = b)", "a ||= b"},
		{"an assignment right side binds looser and needs nothing", "x = a || (a = b)",
			"x = a ||= b"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runLogicalAssignment(test, struct {
				source  string
				options any
			}{testCase.source, nil})
			rule_testing.ExpectFindings(test, result, "logical")
			rule_testing.ExpectFixedSource(test, result,
				logicalAssignmentAsWritten(testCase.source, testCase.want))
		})
	}
}

// TestLogicalAssignmentOperatorsIsSilentInsideReactCompiledFunctions covers the `reactCompiler`
// option, which is ours rather than upstream's.
//
// React Compiler refuses all three shorthands (`Handle ||= operators in AssignmentExpression`,
// measured on babel-plugin-react-compiler 1.0.0 by @system_cohere), in a hook body and in an effect
// callback alike, so suggesting one inside a compiled function costs that function its compilation.
// The three real sites, each modelled here in its original long form, are a callback inside a
// component (UsersRolesPage.tsx:121), a component body (RestEndpointNodeContent.tsx:335), and an
// effect callback inside a provider component (WebSocketViaSharedWorkerProviderInternal.tsx:309).
//
// Every row runs twice, with the compiler on and off, so a silent row is shown to be the gate rather
// than a shape the rule never reported. Which functions count as compiled is the react shelf's
// `IsInsideComponentOrHook`, the same predicate eight react rules use, so a component-named function
// that neither writes JSX nor calls a hook is not compiled and keeps reporting.
func TestLogicalAssignmentOperatorsIsSilentInsideReactCompiledFunctions(test *testing.T) {
	test.Parallel()

	decode := func(raw string) LogicalAssignmentOperatorsOptions {
		decoded, decodeLogicalAssignmentOperatorsOptionsError := DecodeLogicalAssignmentOperatorsOptions(
			json.RawMessage(raw),
		)
		if decodeLogicalAssignmentOperatorsOptionsError != nil {
			test.Fatalf("decoding %s: %v", raw, decodeLogicalAssignmentOperatorsOptionsError)
		}
		options, ok := decoded.(LogicalAssignmentOperatorsOptions)
		if !ok {
			test.Fatalf("decoding %s gave a %T, not LogicalAssignmentOperatorsOptions", raw, decoded)
		}
		return options
	}
	compilerOn := decode(`["always", {"enforceForIfStatements": true}]`)
	compilerOff := decode(`["always", {"enforceForIfStatements": true, "reactCompiler": false}]`)

	cases := []struct {
		name       string
		sourceText string
		compiled   bool
	}{
		{"a callback inside a component, as UsersRolesPage", `
export function UsersRolesPage(properties: { assignments: { type?: string }[] }) {
    const grouped = properties.assignments.reduce(function (groups: Record<string, unknown[]>, assignment) {
        const type = assignment.type;
        if(type) {
            if(!groups[type]) groups[type] = [];
            groups[type].push(assignment);
        }
        return groups;
    }, {});
    return <div>{Object.keys(grouped).length}</div>;
}`, true},
		{"a component body, as RestEndpointNodeContent", `
export function RestEndpointNodeContent(properties: { stored: unknown }) {
    let apiKey = typeof properties.stored === 'string' ? properties.stored : null;
    if(apiKey) apiKey = apiKey.trim();
    return <div>{apiKey}</div>;
}`, true},
		{"an effect callback inside a provider, as WebSocketViaSharedWorkerProviderInternal", `
import React from 'react';
declare function createMonitor(): object;
export function WebSocketProviderInternal(properties: { children: React.ReactNode }) {
    const monitorReference = React.useRef<object | null>(null);
    React.useEffect(function () {
        if(!monitorReference.current) monitorReference.current = createMonitor();
    }, []);
    return <>{properties.children}</>;
}`, true},
		{"a hook body", `
import React from 'react';
export function useCount(initial: number | null) {
    const [count] = React.useState(0);
    let start = initial;
    start = start ?? count;
    return start;
}`, true},
		{"the assignment shape in a component", `
export function Badge(properties: { label?: string }) {
    let label = properties.label;
    label = label || 'none';
    return <span>{label}</span>;
}`, true},
		{"a helper outside any component", `
export function normalizeKey(stored: unknown) {
    let apiKey = typeof stored === 'string' ? stored : null;
    if(apiKey) apiKey = apiKey.trim();
    return apiKey;
}`, false},
		// The name alone does not compile a function: React compiles a component that writes JSX or
		// calls a hook, and this one does neither.
		{"a component-named function with no JSX and no hooks", `
export function Defaults(properties: { label?: string }) {
    let label = properties.label;
    label = label || 'none';
    return label;
}`, false},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			off := rule_testing.RunTypedWithOptions(
				test,
				LogicalAssignmentOperators,
				"Compiled.tsx",
				testCase.sourceText,
				compilerOff,
			)
			if len(off.Diagnostics) == 0 {
				test.Fatalf("with the compiler off the rule must report here, or the row proves nothing")
			}
			on := rule_testing.RunTypedWithOptions(
				test,
				LogicalAssignmentOperators,
				"Compiled.tsx",
				testCase.sourceText,
				compilerOn,
			)
			if testCase.compiled {
				rule_testing.ExpectClean(test, on)
				return
			}
			if len(on.Diagnostics) != len(off.Diagnostics) {
				test.Errorf("outside a compiled function the setting must change nothing: %d findings on, %d off",
					len(on.Diagnostics), len(off.Diagnostics))
			}
		})
	}

	// `never` reports the shorthand and expands it to the long form the compiler accepts, so it is
	// not gated: inside a component it is the rule doing the compiler's work for it.
	never := rule_testing.RunTypedWithOptions(test, LogicalAssignmentOperators, "Compiled.tsx", `
export function Badge(properties: { label?: string }) {
    let label = properties.label;
    label ||= 'none';
    return <span>{label}</span>;
}`, logicalAssignmentNeverOptions())
	rule_testing.ExpectFindings(test, never, "unexpected")
}
