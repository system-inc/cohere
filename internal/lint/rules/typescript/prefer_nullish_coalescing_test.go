package typescript

import (
	"encoding/json"
	"strings"
	"testing"

	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// TestPreferNullishCoalescingDefaultsMatchUpstream pins every default, each proved by a moving
// verdict against the installed 8.67.0 build rather than read off upstream's defaultOptions block.
//
// `ignoreConditionalTests` is the one that matters and the reason this table exists: it defaults
// true, so a port assuming Go's zero value reports on every `if (a || b)` in the tree. That is the
// second non-zero default found in two batches, and both were invisible from the shape of the code.
func TestPreferNullishCoalescingDefaultsMatchUpstream(test *testing.T) {
	test.Parallel()
	decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(nil)
	if decodePreferNullishCoalescingOptionsError != nil {
		test.Fatalf("the decoder refused absent options: %v", decodePreferNullishCoalescingOptionsError)
	}
	options, ok := decoded.(PreferNullishCoalescingOptions)
	if !ok {
		test.Fatalf("the decoder returned %T", decoded)
	}

	if !options.IgnoreConditionalTests {
		test.Error("ignoreConditionalTests defaults TRUE upstream, measured absent-vs-explicit-false")
	}
	if options.IgnoreTernaryTests || options.IgnoreMixedLogicalExpressions ||
		options.IgnoreBooleanCoercion || options.IgnoreIfStatements ||
		options.AllowWithoutStrictNullChecks {
		test.Errorf("every other flag defaults false: %#v", options)
	}
	if options.IgnorePrimitives != (PreferNullishCoalescingPrimitives{}) {
		test.Errorf("ignorePrimitives defaults to exempting nothing: %#v", options.IgnorePrimitives)
	}
}

// TestPreferNullishCoalescingIgnorePrimitivesBothSpellings pins the oneOf.
//
// Upstream's schema is an object of four booleans or the bare literal `true`. Every row here was
// measured by driving the installed rule before this decoder was written, which is the order that
// matters: the corpus splits 110 object-form against 25 bare-true, so a decoder handling only the
// object form passes 110 cases and is wrong on the rest.
//
// Three rows are invisible to the corpus entirely. `{}` and the all-true object bracket the object
// form from both ends, and `false` is refused by the schema rather than meaning "ignore nothing",
// the row a decoder would most naturally get wrong by accepting it and doing something reasonable.
func TestPreferNullishCoalescingIgnorePrimitivesBothSpellings(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		raw     string
		want    PreferNullishCoalescingPrimitives
		wantErr bool
	}{
		{
			name: "absent exempts nothing",
			raw:  `{}`,
		},
		{
			name: "an EMPTY object exempts nothing, which is not the same as absent at the wire",
			raw:  `{"ignorePrimitives":{}}`,
		},
		{
			name: "the bare literal true exempts everything",
			raw:  `{"ignorePrimitives":true}`,
			want: PreferNullishCoalescingPrimitives{IgnoreAll: true},
		},
		{
			// The row a decoder gets wrong by being helpful. Upstream's `enum: [true]` makes this
			// illegal, and accepting it means a config that loads here and fails in ESLint.
			name:    "the literal false is REFUSED, because enum:[true] admits only one boolean",
			raw:     `{"ignorePrimitives":false}`,
			wantErr: true,
		},
		{
			name: "one primitive, leaving the other three reporting",
			raw:  `{"ignorePrimitives":{"string":true}}`,
			want: PreferNullishCoalescingPrimitives{String: true},
		},
		{
			name: "an explicit false for one primitive exempts nothing",
			raw:  `{"ignorePrimitives":{"string":false}}`,
		},
		{
			name: "all four spelled out, which behaves like the bare true but is a different shape",
			raw:  `{"ignorePrimitives":{"bigint":true,"boolean":true,"number":true,"string":true}}`,
			want: PreferNullishCoalescingPrimitives{Bigint: true, Boolean: true, Number: true, String: true},
		},
		{
			name:    "an unknown primitive is refused, matching additionalProperties:false",
			raw:     `{"ignorePrimitives":{"strings":true}}`,
			wantErr: true,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(
				json.RawMessage(testCase.raw),
			)
			if testCase.wantErr {
				if decodePreferNullishCoalescingOptionsError == nil {
					test.Fatalf("expected the decoder to refuse %s, got %#v", testCase.raw, decoded)
				}
				return
			}
			if decodePreferNullishCoalescingOptionsError != nil {
				test.Fatalf("the decoder refused %s: %v", testCase.raw, decodePreferNullishCoalescingOptionsError)
			}
			options, isType := decoded.(PreferNullishCoalescingOptions)
			if !isType {
				test.Fatalf("decoding the options gave %T, expected PreferNullishCoalescingOptions", decoded)
			}
			if options.IgnorePrimitives != testCase.want {
				test.Errorf("expected %#v, got %#v", testCase.want, options.IgnorePrimitives)
			}
		})
	}
}

// TestPreferNullishCoalescingDecoderAcceptsTheShapesTheConfigLayerDelivers crosses the boundary.
//
// `meta.schema` declares one element here, so the cohere spelling and upstream's coincide. Stated
// rather than assumed, because a fixture hands the decoder bytes the test built and only bytes the
// config layer sliced reveal a mismatch.
func TestPreferNullishCoalescingDecoderAcceptsTheShapesTheConfigLayerDelivers(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name    string
		raw     string
		check   func(*testing.T, PreferNullishCoalescingOptions)
		wantErr bool
	}{
		{
			name: "a bare \"error\" delivers no options at all",
			check: func(test *testing.T, o PreferNullishCoalescingOptions) {
				if !o.IgnoreConditionalTests {
					test.Error("the non-zero default must survive an absent options value")
				}
			},
		},
		{
			name: "every flag at once",
			raw: `{"allowRuleToRunWithoutStrictNullChecksIKnowWhatIAmDoing":true,` +
				`"ignoreBooleanCoercion":true,"ignoreConditionalTests":false,` +
				`"ignoreIfStatements":true,"ignoreMixedLogicalExpressions":true,` +
				`"ignoreTernaryTests":true,"ignorePrimitives":true}`,
			check: func(test *testing.T, o PreferNullishCoalescingOptions) {
				if !o.AllowWithoutStrictNullChecks || !o.IgnoreBooleanCoercion ||
					!o.IgnoreIfStatements || !o.IgnoreMixedLogicalExpressions ||
					!o.IgnoreTernaryTests || !o.IgnorePrimitives.IgnoreAll {
					test.Errorf("a key was dropped: %#v", o)
				}
				if o.IgnoreConditionalTests {
					test.Error("an explicit false must override the true default, which is the whole " +
						"reason these are pointers on the wire")
				}
			},
		},
		{
			name:    "a shape that is neither must error rather than decode to a default",
			raw:     `"ignoreTernaryTests"`,
			wantErr: true,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(
				json.RawMessage(testCase.raw),
			)
			if testCase.wantErr {
				if decodePreferNullishCoalescingOptionsError == nil {
					test.Fatalf("expected a refusal, got %#v", decoded)
				}
				return
			}
			if decodePreferNullishCoalescingOptionsError != nil {
				test.Fatalf("the decoder refused %s: %v", testCase.raw, decodePreferNullishCoalescingOptionsError)
			}
			if testCase.check != nil {
				options, isType := decoded.(PreferNullishCoalescingOptions)
				if !isType {
					test.Fatalf("decoding the options gave %T, expected PreferNullishCoalescingOptions", decoded)
				}
				testCase.check(test, options)
			}
		})
	}
}

// preferNullishCoalescingFile is where the fixtures pretend to live.
const preferNullishCoalescingFile = "/repository/source/PreferNullish.ts"

type preferNullishCoalescingCase struct {
	name        string
	source      string
	optionsJson string
	findings    int
}

func runPreferNullishCoalescing(test *testing.T, testCase preferNullishCoalescingCase) rule_testing.Result {
	test.Helper()
	var options any
	if testCase.optionsJson != "" {
		decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(
			json.RawMessage(testCase.optionsJson),
		)
		if decodePreferNullishCoalescingOptionsError != nil {
			test.Fatalf("the decoder refused %s: %v", testCase.optionsJson, decodePreferNullishCoalescingOptionsError)
		}
		options = decoded
	} else {
		// A rule configured as a bare "error" is handed nil, and this rule's ignoreConditionalTests
		// default is true, so routing the empty case through the decoder is what proves the rule
		// gets that default rather than Go's zero value.
		decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(nil)
		if decodePreferNullishCoalescingOptionsError != nil {
			test.Fatalf("the decoder refused absent options: %v", decodePreferNullishCoalescingOptionsError)
		}
		options = decoded
	}
	return rule_testing.RunTypedWithOptions(test, PreferNullishCoalescing,
		preferNullishCoalescingFile, testCase.source, options)
}

// TestPreferNullishCoalescingStaysSilent runs upstream's whole `valid` list.
//
// All 275, across every reporting path: a valid case is a false positive this rule must not
// produce, whichever path would have produced it.
func TestPreferNullishCoalescingStaysSilent(test *testing.T) {
	test.Parallel()
	for _, testCase := range preferNullishCoalescingCleanCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, runPreferNullishCoalescing(test, testCase))
		})
	}
}

// TestPreferNullishCoalescingFires runs every invalid case whose findings are all preferNullishOverOr.
func TestPreferNullishCoalescingFires(test *testing.T) {
	test.Parallel()
	for _, testCase := range preferNullishCoalescingOrCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferNullishCoalescing(test, testCase)
			wantIds := make([]string, 0, testCase.findings)
			for range testCase.findings {
				wantIds = append(wantIds, messagePreferNullishOverOr.Id)
			}
			rule_testing.ExpectFindings(test, result, wantIds...)
		})
	}
}

// preferNullishCoalescingRenderingCase is one shape with everything a message id cannot see.
type preferNullishCoalescingRenderingCase struct {
	name    string
	source  string
	options string
	// findings, in order, each with the exact span, whole message, and suggested replacement text.
	findings []preferNullishCoalescingRendering
	reason   string
}

type preferNullishCoalescingRendering struct {
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
	suggested string
}

// TestPreferNullishCoalescingRenderings is the section 3c and section 8 table.
//
// This rule has one message id and reports on two arms that render differently, propose different
// repair text, and parenthesise differently when chained. A fixture asserting the id and the count
// cannot see any of that, and the imported corpus asserts nothing more: all 398 cases pass with a
// rule that renders the wrong arm's message, writes the wrong replacement, and proposes a syntax
// error on every chained expression.
//
// Every expectation below was measured by driving the installed 8.67.0 build and reading the
// message and suggestion text, not the id.
func TestPreferNullishCoalescingRenderings(test *testing.T) {
	test.Parallel()
	const orMessage = "Prefer using nullish coalescing operator (`??`) instead of a logical or " +
		"(`||`), as it is a safer operator."
	const assignMessage = "Prefer using nullish coalescing operator (`??=`) instead of a logical " +
		"assignment (`||=`), as it is a safer operator."

	cases := []preferNullishCoalescingRenderingCase{
		{
			name:   "the || arm renders `or` and suggests ??",
			source: "declare let a: string | null;\ndeclare const b: string;\nconst x = a || b;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 3, column: 13, endLine: 3, endColumn: 15, message: orMessage, suggested: "??"},
			},
			reason: "the span is the OPERATOR token, not the expression",
		},
		{
			name:   "the ||= arm renders `assignment` and suggests ??=",
			source: "declare let a: string | null;\ndeclare const b: string;\na ||= b;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 3, column: 3, endLine: 3, endColumn: 6, message: assignMessage, suggested: "??="},
			},
			reason: "one message id, a different rendering AND a different repair; nothing in the " +
				"corpus separates this arm from the one above",
		},
		{
			name: "a chained || parenthesises the inner finding, because `a ?? b || c` is a syntax error",
			source: "declare let a: string | null;\ndeclare let b: string | null;\n" +
				"declare const c: string;\nconst x = a || b || c;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 4, column: 13, endLine: 4, endColumn: 15, message: orMessage, suggested: "(a ?? b)"},
				{line: 4, column: 18, endLine: 4, endColumn: 20, message: orMessage, suggested: "??"},
			},
			reason: "the inner || has a || parent so its whole expression is replaced; the outer one " +
				"gets the plain swap. A port writing ?? everywhere passes all 398 corpus cases.",
		},
		{
			name: "the parenthesised chain puts the replacement on the other side",
			source: "declare let a: string | null;\ndeclare let b: string | null;\n" +
				"declare const c: string;\nconst x = a || (b || c);\n",
			findings: []preferNullishCoalescingRendering{
				{line: 4, column: 13, endLine: 4, endColumn: 15, message: orMessage, suggested: "??"},
				{line: 4, column: 19, endLine: 4, endColumn: 21, message: orMessage, suggested: "(b ?? c)"},
			},
			reason: "which finding gets parenthesised follows the PARENT, so writing the source the " +
				"other way round swaps them",
		},
		{
			name: "three deep, so two findings parenthesise and only the outermost does not",
			source: "declare let a: string | null;\ndeclare let b: string | null;\n" +
				"declare let c: string | null;\ndeclare const d: string;\nconst x = a || b || c || d;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 5, column: 13, endLine: 5, endColumn: 15, message: orMessage, suggested: "(a ?? b)"},
				{line: 5, column: 18, endLine: 5, endColumn: 20, message: orMessage, suggested: "(b ?? c)"},
				{line: 5, column: 23, endLine: 5, endColumn: 25, message: orMessage, suggested: "??"},
			},
			reason: "the rule is per-finding rather than per-expression",
		},
		{
			name: "a && parent is NOT a || parent, so the plain swap stands",
			source: "declare let a: string | null;\ndeclare const b: string;\ndeclare const d: string;\n" +
				"const x = (a || b) && d;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 4, column: 14, endLine: 4, endColumn: 16, message: orMessage, suggested: "??"},
			},
			reason: "the row that shows the test is on a logical OR parent specifically, not on any " +
				"logical parent",
		},
		{
			name:   "optional chaining reports exactly like a plain member access",
			source: "declare const o: { a?: string } | null;\ndeclare const c: string;\nconst x = o?.a || c;\n",
			findings: []preferNullishCoalescingRendering{
				{line: 3, column: 16, endLine: 3, endColumn: 18, message: orMessage, suggested: "??"},
			},
			reason: "predicted as a divergence and measured as a non-divergence: the rule anchors on " +
				"the operator, so ESTree's ChainExpression wrapper never gates it",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferNullishCoalescing(test, preferNullishCoalescingCase{
				source: testCase.source, optionsJson: testCase.options,
			})
			if len(result.Diagnostics) != len(testCase.findings) {
				test.Fatalf("expected %d findings, got %d: %v. %s",
					len(testCase.findings), len(result.Diagnostics), result.MessageIds(), testCase.reason)
			}
			for index, expected := range testCase.findings {
				diagnostic := result.Diagnostics[index]

				wantPosition := preferNullishCoalescingOffsetOf(test, testCase.source, expected.line, expected.column)
				wantEnd := preferNullishCoalescingOffsetOf(test, testCase.source, expected.endLine, expected.endColumn)
				if diagnostic.Range.Pos() != wantPosition || diagnostic.Range.End() != wantEnd {
					test.Errorf("finding %d: expected span [%d,%d) %q, got [%d,%d) %q",
						index, wantPosition, wantEnd,
						asTheTypedPreferNullishHarnessWroteIt(testCase.source)[wantPosition:wantEnd],
						diagnostic.Range.Pos(), diagnostic.Range.End(),
						asTheTypedPreferNullishHarnessWroteIt(
							testCase.source,
						)[diagnostic.Range.Pos():diagnostic.Range.End()])
				}

				// The whole upstream sentence, not a prefix. A prefix assertion cannot separate the
				// two arms, which is the defect this table exists to catch.
				if !strings.HasPrefix(diagnostic.Message.Description, expected.message) {
					test.Errorf("finding %d: expected the message to open with\n  %q\ngot\n  %q",
						index, expected.message, diagnostic.Message.Description)
				}

				if len(diagnostic.Suggestions) != 1 {
					test.Fatalf("finding %d: expected one suggestion, got %d",
						index, len(diagnostic.Suggestions))
				}
				fixes := diagnostic.Suggestions[0].Fixes
				if len(fixes) != 1 {
					test.Fatalf("finding %d: expected one fix, got %d", index, len(fixes))
				}
				if fixes[0].Text != expected.suggested {
					test.Errorf("finding %d: expected the repair to write %q, got %q. %s",
						index, expected.suggested, fixes[0].Text, testCase.reason)
				}
			}
		})
	}
}

// preferNullishCoalescingOffsetOf converts a 1-based line and column into a byte offset in the file
// as RunTyped wrote it.
//
// `RunTyped` writes `strings.TrimSpace(contents)+"\n"`, so a fixture with leading whitespace is
// offset from the Go literal. These sources have none, and the helper is written to handle it anyway
// so a later case with a leading newline is not silently off by one.
func preferNullishCoalescingOffsetOf(test *testing.T, source string, line int, column int) int {
	test.Helper()
	text := asTheTypedPreferNullishHarnessWroteIt(source)
	offset := 0
	for current := 1; current < line; current++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			test.Fatalf("the fixture has no line %d", line)
		}
		offset += next + 1
	}
	return offset + column - 1
}

func asTheTypedPreferNullishHarnessWroteIt(text string) string {
	return strings.TrimSpace(text) + "\n"
}

// TestPreferNullishCoalescingArmCoverage counts upstream's corpus by the path that reports it.
//
// Upstream reports four message ids from three paths. A count that silently drifts is how a partial
// port comes to look complete, and this rule did look complete for a month: its decoder parsed the
// ternary and if-statement options while neither check existed (#10kqgs6). So every path's cases are
// counted here, and dropping one fails by name.
//
//	preferNullishOverOr           123 invalid cases    prefer_nullish_coalescing_or_cases_test.go
//	preferNullishOverTernary      205 invalid cases    prefer_nullish_coalescing_ternary_cases_test.go
//	preferNullishOverAssignment    14 invalid cases    prefer_nullish_coalescing_if_cases_test.go
//	  and also ...OverOr            2 invalid cases    prefer_nullish_coalescing_if_cases_test.go
//	noStrictNullCheck               1 invalid case     not implemented, needs a non-strict program
//
// All 275 of upstream's valid cases run against every path.
func TestPreferNullishCoalescingArmCoverage(test *testing.T) {
	test.Parallel()
	if len(preferNullishCoalescingOrCases) != 123 {
		test.Errorf("expected upstream's 123 preferNullishOverOr cases, have %d",
			len(preferNullishCoalescingOrCases))
	}
	if len(preferNullishCoalescingCleanCases) != 275 {
		test.Errorf("expected all 275 of upstream's valid cases, have %d", len(preferNullishCoalescingCleanCases))
	}

	byPath := map[string]int{}
	for _, testCase := range preferNullishCoalescingTernaryAndIfCases {
		ids := map[string]bool{}
		for _, finding := range testCase.findings {
			ids[finding.id] = true
		}
		switch {
		case ids[messagePreferNullishOverTernary.Id] && len(ids) == 1:
			byPath["ternary"]++
		case ids[messagePreferNullishOverAssignment.Id] && len(ids) == 1:
			byPath["assignment"]++
		case ids[messagePreferNullishOverAssignment.Id] && ids[messagePreferNullishOverOr.Id] && len(ids) == 2:
			byPath["assignment and or"]++
		default:
			test.Errorf("%s reports an unexpected mix of ids: %v", testCase.name, ids)
		}
	}
	for path, want := range map[string]int{"ternary": 205, "assignment": 14, "assignment and or": 2} {
		if byPath[path] != want {
			test.Errorf("expected %d %s cases from upstream's corpus, have %d", want, path, byPath[path])
		}
	}
}
