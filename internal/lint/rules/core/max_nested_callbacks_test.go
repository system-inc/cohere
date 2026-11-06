package core

import (
	"fmt"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// The corpus is ESLint's own, 13 valid and 19 invalid cases carrying 20 findings, extracted by
// loading its test file with a stubbed rule tester so nothing was retyped.
//
// Options are routed through DecodeMaxNestedCallbacksOptions rather than built as a struct, because
// the decoder is where this port's two hardest lines live: the polymorphic integer-or-object wire
// shape, and upstream's truthiness resolution of `maximum` against `max`. A fixture handing the
// rule a struct would leave both untested.
//
// The wire value is the bare first element of upstream's options array, because cohere's config
// layer strips the severity tuple before dispatch.

// decodeMaxNestedCallbacksForTest turns a fixture's JSON into options the way the config layer does.
// An empty string stands for a rule configured as a bare severity, which reaches the decoder as
// empty input.
func decodeMaxNestedCallbacksForTest(test *testing.T, optionsJson string) MaxNestedCallbacksOptions {
	test.Helper()
	decoded, decodeMaximumNestedCallbacksOptionsError := DecodeMaxNestedCallbacksOptions([]byte(optionsJson))
	if decodeMaximumNestedCallbacksOptionsError != nil {
		test.Fatalf("decoding %q: %v", optionsJson, decodeMaximumNestedCallbacksOptionsError)
	}
	return decoded
}

func TestMaxNestedCallbacksStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		source      string
		optionsJson string
	}{
		{name: "valid0", source: "foo(function() { bar(thing, function(data) {}); });", optionsJson: "3"},
		{
			name:        "valid1",
			source:      "var foo = function() {}; bar(function(){ baz(function() { qux(foo); }) });",
			optionsJson: "2",
		},
		{name: "valid2", source: "fn(function(){}, function(){}, function(){});", optionsJson: "2"},
		{name: "valid3", source: "fn(() => {}, function(){}, function(){});", optionsJson: "2"},
		{
			name:        "valid4",
			source:      "foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {});});});});});});});});});});",
			optionsJson: "",
		},
		{name: "valid5", source: "foo(function() { bar(thing, function(data) {}); });", optionsJson: "{\"max\": 3}"},
		{name: "valid6", source: "(() => {})();", optionsJson: "{\"max\": 0}"},
		{name: "valid7", source: "(function() {})();", optionsJson: "{\"max\": 0}"},
		{name: "valid8", source: "new Promise(() => {});", optionsJson: "0"},
		{name: "valid9", source: "new Promise(() => {});", optionsJson: "{\"max\": 0}"},
		{
			name:        "valid10",
			source:      "new Promise(() => {});",
			optionsJson: "{\"max\": 0, \"checkConstructorCallCallbacks\": false}",
		},
		{
			name:        "valid11",
			source:      "new (() => {})();",
			optionsJson: "{\"max\": 0, \"checkConstructorCallCallbacks\": true}",
		},
		{
			name:        "valid12",
			source:      "new Promise(() => {});",
			optionsJson: "{\"max\": 1, \"checkConstructorCallCallbacks\": true}",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunWithOptions(test, MaxNestedCallbacks,
				"file.ts", testCase.source, decodeMaxNestedCallbacksForTest(test, testCase.optionsJson)))
		})
	}
}

// wantSpan is one expected finding: where it points and what text that covers.
type wantSpan struct {
	position int
	end      int
	text     string
}

// Each invalid case asserts the finding count, every span, the text each span covers, and the
// rendered message. ExpectFindings would see none of those: the message interpolates two integers
// that both move, and passing them in the wrong order renders a grammatical sentence.
func TestMaxNestedCallbacksFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		source      string
		optionsJson string
		wantSpans   []wantSpan
		wantCounts  [][2]int
	}{
		{
			name:        "invalid0",
			source:      "foo(function() { bar(thing, function(data) { baz(function() {}); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{49, 57, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid1",
			source:      "foo(function() { const helper = function() {}; bar(function() { baz(function() {}); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{68, 76, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid2",
			source:      "foo(function() { const helper = () => {}; bar(function() { baz(function() {}); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{63, 71, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid3",
			source:      "foo(function() { bar(function() { baz(function() { qux(function() {}); }); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{38, 46, "function"}, {55, 63, "function"}},
			wantCounts:  [][2]int{{3, 2}, {4, 2}},
		},
		{
			name:        "invalid4",
			source:      "foo(function() { bar(function() { baz(function() { const qux = function() {}; }); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{38, 46, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid5",
			source:      "foo(function() { bar(thing, (data) => { baz(function() {}); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{44, 52, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid6",
			source:      "foo(() => { bar(thing, (data) => { baz( () => {}); }); });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{43, 45, "=>"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid7",
			source:      "foo(function() { if (isTrue) { bar(function(data) { baz(function() {}); }); } });",
			optionsJson: "2",
			wantSpans:   []wantSpan{{56, 64, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid8",
			source:      "foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {});});});});});});});});});});});",
			optionsJson: "",
			wantSpans:   []wantSpan{{164, 172, "function"}},
			wantCounts:  [][2]int{{11, 10}},
		},
		{
			name:        "invalid9",
			source:      "foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {foo(function() {});});});});});});});});});});});",
			optionsJson: "{}",
			wantSpans:   []wantSpan{{164, 172, "function"}},
			wantCounts:  [][2]int{{11, 10}},
		},
		{
			name:        "invalid10",
			source:      "foo(function() {})",
			optionsJson: "{\"max\": 0}",
			wantSpans:   []wantSpan{{4, 12, "function"}},
			wantCounts:  [][2]int{{1, 0}},
		},
		{
			name:        "invalid11",
			source:      "foo(function() { bar(thing, function(data) { baz(function() {}); }); });",
			optionsJson: "{\"max\": 2}",
			wantSpans:   []wantSpan{{49, 57, "function"}},
			wantCounts:  [][2]int{{3, 2}},
		},
		{
			name:        "invalid12",
			source:      "fn('before', () => 'counted', 'after');",
			optionsJson: "{\"max\": 0}",
			wantSpans:   []wantSpan{{16, 18, "=>"}},
			wantCounts:  [][2]int{{1, 0}},
		},
		{
			name:        "invalid13",
			source:      "object.method(() => 'counted');",
			optionsJson: "{\"max\": 0}",
			wantSpans:   []wantSpan{{17, 19, "=>"}},
			wantCounts:  [][2]int{{1, 0}},
		},
		{
			name:        "invalid14",
			source:      "(() => {})(() => 'counted');",
			optionsJson: "{\"max\": 0}",
			wantSpans:   []wantSpan{{14, 16, "=>"}},
			wantCounts:  [][2]int{{1, 0}},
		},
		{
			name:        "invalid15",
			source:      "new Promise(() => {});",
			optionsJson: "{\"max\": 0, \"checkConstructorCallCallbacks\": true}",
			wantSpans:   []wantSpan{{15, 17, "=>"}},
			wantCounts:  [][2]int{{1, 0}},
		},
		{
			name:        "invalid16",
			source:      "fn(() => { new Promise(() => {}); });",
			optionsJson: "{\"max\": 1, \"checkConstructorCallCallbacks\": true}",
			wantSpans:   []wantSpan{{26, 28, "=>"}},
			wantCounts:  [][2]int{{2, 1}},
		},
		{
			name:        "invalid17",
			source:      "new Promise(() => { fn(() => {}); });",
			optionsJson: "{\"max\": 1, \"checkConstructorCallCallbacks\": true}",
			wantSpans:   []wantSpan{{26, 28, "=>"}},
			wantCounts:  [][2]int{{2, 1}},
		},
		{
			name:        "invalid18",
			source:      "new Promise(() => { new Promise(() => {}); });",
			optionsJson: "{\"max\": 1, \"checkConstructorCallCallbacks\": true}",
			wantSpans:   []wantSpan{{35, 37, "=>"}},
			wantCounts:  [][2]int{{2, 1}},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts",
				testCase.source, decodeMaxNestedCallbacksForTest(test, testCase.optionsJson))
			if len(result.Diagnostics) != len(testCase.wantSpans) {
				test.Fatalf("expected %d findings, got %d", len(testCase.wantSpans), len(result.Diagnostics))
			}
			for index, finding := range result.Diagnostics {
				want := testCase.wantSpans[index]
				if finding.Range.Pos() != want.position || finding.Range.End() != want.end {
					test.Errorf("finding %d span: got [%d,%d), want [%d,%d)",
						index, finding.Range.Pos(), finding.Range.End(), want.position, want.end)
				}
				if got := testCase.source[finding.Range.Pos():finding.Range.End()]; got != want.text {
					test.Errorf("finding %d text: got %q, want %q", index, got, want.text)
				}
				if finding.Message.Id != "exceed" {
					test.Errorf("finding %d id: got %q, want %q", index, finding.Message.Id, "exceed")
				}
				// The prefix is upstream's whole rendered sentence; our description continues past
				// it with the reasoning, so the assertion is on the part upstream specifies.
				wantPrefix := fmt.Sprintf("Too many nested callbacks (%d). Maximum allowed is %d. ",
					testCase.wantCounts[index][0], testCase.wantCounts[index][1])
				if got := finding.Message.Description; len(got) < len(wantPrefix) ||
					got[:len(wantPrefix)] != wantPrefix {
					test.Errorf("finding %d message:\n got %q\nwant prefix %q", index, got, wantPrefix)
				}
			}
		})
	}
}

// The decoder's own decisions, each measured against the installed build at 10.8.1 before being
// written here. None of these has a counterpart in upstream's corpus, and each is a line that would
// pass every imported case while being wrong.
func TestDecodeMaxNestedCallbacksOptions(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name                              string
		optionsJson                       string
		wantMaximum                       int
		wantCheckConstructorCallCallbacks bool
	}{
		{"absentMeansTen", "", 10, false},
		{"bareInteger", "3", 3, false},
		{"bareZeroIsALimitNotAnAbsence", "0", 0, false},
		{"objectMax", `{"max": 3}`, 3, false},
		{"objectMaximum", `{"maximum": 3}`, 3, false},
		{"emptyObjectMeansTen", "{}", 10, false},
		{"objectWithOnlyTheFlagMeansTen", `{"checkConstructorCallCallbacks": true}`, 10, true},
		// `option.maximum || option.max` is a truthiness test, so a zero `maximum` falls through
		// to `max` and the user who asked for zero gets five. Measured clean upstream on a
		// one-deep callback under this configuration, which is only possible if 5 is in force.
		{"zeroMaximumFallsThroughToMax", `{"maximum": 0, "max": 5}`, 5, false},
		{"maximumWinsWhenBothAreTruthy", `{"maximum": 1, "max": 5}`, 1, false},
		{"bothZeroLeavesZero", `{"maximum": 0, "max": 0}`, 0, false},
		{"flagOff", `{"max": 0, "checkConstructorCallCallbacks": false}`, 0, false},
		{"flagOn", `{"max": 0, "checkConstructorCallCallbacks": true}`, 0, true},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeMaximumNestedCallbacksOptionsError := DecodeMaxNestedCallbacksOptions(
				[]byte(testCase.optionsJson),
			)
			if decodeMaximumNestedCallbacksOptionsError != nil {
				test.Fatalf("decoding %q: %v", testCase.optionsJson, decodeMaximumNestedCallbacksOptionsError)
			}
			if decoded.Maximum == nil {
				test.Fatalf("maximum is nil, which would leave the rule inert")
			}
			if *decoded.Maximum != testCase.wantMaximum {
				test.Errorf("maximum: got %d, want %d", *decoded.Maximum, testCase.wantMaximum)
			}
			got := decoded.CheckConstructorCallCallbacks != nil && *decoded.CheckConstructorCallCallbacks
			if got != testCase.wantCheckConstructorCallCallbacks {
				test.Errorf("checkConstructorCallCallbacks: got %v, want %v", got,
					testCase.wantCheckConstructorCallCallbacks)
			}
		})
	}
}

// A rule configured as a bare severity reaches Run unconfigured rather than with options, and a
// zero-value struct there carries a nil Maximum. Without the fallback the rule reads the limit as
// zero and reports every callback in the tree, or reads it as nil and reports nothing; either way
// every fixture above still passes, because all of them route through the decoder.
func TestMaxNestedCallbacksHandlesNilOptions(test *testing.T) {
	test.Parallel()

	deepEnoughForTheDefault := "foo(function(){"
	for depth := 0; depth < 10; depth++ {
		deepEnoughForTheDefault += " foo(function(){"
	}
	for depth := 0; depth < 11; depth++ {
		deepEnoughForTheDefault += "})"
	}
	result := rule_testing.Run(test, MaxNestedCallbacks, "file.ts", deepEnoughForTheDefault)
	if len(result.Diagnostics) != 1 {
		test.Fatalf("expected the default limit of 10 to report once on an 11-deep nest, got %d findings",
			len(result.Diagnostics))
	}
	shallow := "foo(function(){ foo(function(){}) })"
	rule_testing.ExpectClean(test, rule_testing.Run(test, MaxNestedCallbacks, "file.ts", shallow))
	// And the same input through the decoder, so the two paths are pinned to the same answer.
	decoded, decodeMaximumNestedCallbacksOptionsError := DecodeMaxNestedCallbacksOptions([]byte(""))
	if decodeMaximumNestedCallbacksOptionsError != nil {
		test.Fatalf("decoding empty: %v", decodeMaximumNestedCallbacksOptionsError)
	}
	if len(rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts",
		deepEnoughForTheDefault, decoded).Diagnostics) != 1 {
		test.Errorf("the decoder path and the unconfigured path disagree about the default")
	}
}

// Cases upstream's corpus cannot express, because its parser deletes the node that makes them
// different. Each was measured against the installed build at 10.8.1 before being written here.
//
// Upstream writes two immediately invoked expressions as passing cases, and both are parenthesized:
// `(function(){})()` and `(() => {})()`. Its parser folds the parenthesis away, so the function
// becomes the call's direct callee and its `parent.callee === node` test is what makes them clean.
// Ours keeps a ParenthesizedExpression between the two, so the parent is not a call at all and the
// callee test is never consulted for those inputs. A mutant deleting the callee test survived both
// of them and the whole rest of the corpus.
//
// The shape that separates them is an immediately invoked function reached through a unary or
// binary operator instead of parens, where the function really is the direct callee here too.
func TestMaxNestedCallbacksDirectCalleeIsNotACallback(test *testing.T) {
	test.Parallel()

	zero := decodeMaxNestedCallbacksForTest(test, "0")

	silent := []struct {
		name   string
		source string
	}{
		{"bangImmediatelyInvoked", "!function(){}();"},
		{"voidImmediatelyInvoked", "void function(){}();"},
		{"assignedImmediatelyInvoked", "x = function(){}();"},
		{"typeofImmediatelyInvoked", "typeof function(){}();"},
		{"parenthesizedImmediatelyInvoked", "(function(){})();"},
		{"parenthesizedArrowImmediatelyInvoked", "(() => {})();"},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts", testCase.source, zero))
		})
	}

	// The callee is still not counted while its own argument is, so this reports exactly once and
	// points at the argument. A rule counting the callee would report twice.
	test.Run("theArgumentOfADirectCalleeStillCounts", func(test *testing.T) {
		test.Parallel()
		source := "!function(){}(function(){});"
		result := rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts", source, zero)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("expected 1 finding, got %d", len(result.Diagnostics))
		}
		if got := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]; got != "function" {
			test.Errorf("reported text: got %q, want %q", got, "function")
		}
		if result.Diagnostics[0].Range.Pos() != 14 {
			test.Errorf("span points at %d, want 14, which is the argument rather than the callee",
				result.Diagnostics[0].Range.Pos())
		}
	})
}

// The message id, asserted through the harness helper the fixture-pair guard looks for.
//
// The corpus tests above assert the finding count, every span, and the rendered text, which is
// strictly more than a message-id assertion proves. This exists because none of that names the id
// through rule_testing.ExpectFindings, and the guard that checks a rule can be shown to fire reads
// the test file textually. It is a real assertion rather than a formality: an id typo would render
// a correct sentence and every span assertion above would still pass.
func TestMaxNestedCallbacksReportsTheExceedId(test *testing.T) {
	test.Parallel()

	source := "foo(function() { bar(thing, function(data) { baz(function() {}); }); });"
	rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts",
		source, decodeMaxNestedCallbacksForTest(test, "2")), "exceed")

	// Two findings from one input, so the helper is exercised on a count other than one.
	nested := "foo(function() { bar(function() { baz(function() { qux(function() {}); }); }); });"
	rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, MaxNestedCallbacks, "file.ts",
		nested, decodeMaxNestedCallbacksForTest(test, "2")), "exceed", "exceed")
}
