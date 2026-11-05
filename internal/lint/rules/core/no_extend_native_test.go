package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// extendNativeFile is where the fixtures pretend to live.
const extendNativeFile = "/repository/source/ExtendNative.ts"

// The corpus is ESLint's own, extracted rather than retyped.
//
// Every case is verbatim from `eslint/tests/lib/rules/no-extend-native.js` at eslint 10.8.1: 19
// valid and 21 invalid. Extracted by executing that file against a stub RuleTester, with each case's
// own `options` and each finding's own `data.builtin` carried across, so the expectations are
// upstream's rather than recovered. All 40 were additionally driven through the installed build.
//
// The message interpolates the builtin's name, so the invalid table names the expected builtin per
// finding rather than only the message id: `ExpectFindings` cannot see anything the format string
// does, and one input reports twice with a different name each time.
func TestNoExtendNativeFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    rule.Given[NoExtendNativeOptions]
		builtins   []string
	}{
		{"Object.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"Object"}},
		{"BigInt.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"BigInt"}},
		{"WeakRef.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"WeakRef"}},
		{"FinalizationRegistry.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"FinalizationRegistry"}},
		{"AggregateError.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"AggregateError"}},
		{"Function.prototype['p'] = 0", rule.Given[NoExtendNativeOptions]{}, []string{"Function"}},
		{"String['prototype'].p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"String"}},
		{"Number['prototype']['p'] = 0", rule.Given[NoExtendNativeOptions]{}, []string{"Number"}},
		{
			"Object.defineProperty(Array.prototype, 'p', {value: 0})",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Array"},
		},
		{
			"Object.defineProperties(Array.prototype, {p: {value: 0}})",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Array"},
		},
		{
			"Object.defineProperties(Array.prototype, {p: {value: 0}, q: {value: 0}})",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Array"},
		},
		{
			"Number['prototype']['p'] = 0",
			rule.Configured(NoExtendNativeOptions{Exceptions: []string{"Object"}}),
			[]string{"Number"},
		},
		{
			"Object.prototype.p = 0; Object.prototype.q = 0",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Object", "Object"},
		},
		{"function foo() { Object.prototype.p = 0 }", rule.Given[NoExtendNativeOptions]{}, []string{"Object"}},
		{"(Object?.prototype).p = 0", rule.Given[NoExtendNativeOptions]{}, []string{"Object"}},
		{
			"Object.defineProperty(Object?.prototype, 'p', { value: 0 })",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Object"},
		},
		{
			"Object?.defineProperty(Object.prototype, 'p', { value: 0 })",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Object"},
		},
		{
			"(Object?.defineProperty)(Object.prototype, 'p', { value: 0 })",
			rule.Given[NoExtendNativeOptions]{},
			[]string{"Object"},
		},
		{"Array.prototype.p &&= 0", rule.Given[NoExtendNativeOptions]{}, []string{"Array"}},
		{"Array.prototype.p ||= 0", rule.Given[NoExtendNativeOptions]{}, []string{"Array"}},
		{"Array.prototype.p ??= 0", rule.Given[NoExtendNativeOptions]{}, []string{"Array"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedGiven(test, NoExtendNative, extendNativeFile,
				testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != len(testCase.builtins) {
				test.Fatalf("wanted %d diagnostics, got %d", len(testCase.builtins), len(result.Diagnostics))
			}
			for index, wantBuiltin := range testCase.builtins {
				diagnostic := result.Diagnostics[index]
				if diagnostic.Message.Id != "unexpected" {
					test.Fatalf("finding %d had id %q", index, diagnostic.Message.Id)
				}
				// The interpolated name, which the id assertion cannot see. A rule reusing the
				// wrong variable in the format slot passes every id fixture in this file.
				if want := "`" + wantBuiltin + ".prototype`"; !strings.Contains(
					diagnostic.Message.Description, want) {
					test.Fatalf("finding %d did not name %s: %q", index, want, diagnostic.Message.Description)
				}
			}
		})
	}
}

// The clean cases, with their own options.
//
// Four groups: a receiver that is not a builtin, a builtin reached through something other than its
// own identifier, a shadowed builtin, and a builtin named in the `exceptions` option. The
// `parseFloat.prototype.x = 1` row is the one that pins the uppercase-first filter on the builtin
// set: `parseFloat` is a global and its prototype is being extended, and it is clean purely because
// its name does not start with a capital.
func TestNoExtendNativeStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    rule.Given[NoExtendNativeOptions]
	}{
		{"x.prototype.p = 0", rule.Given[NoExtendNativeOptions]{}},
		{"x.prototype['p'] = 0", rule.Given[NoExtendNativeOptions]{}},
		{"Object.p = 0", rule.Given[NoExtendNativeOptions]{}},
		{"Object.toString.bind = 0", rule.Given[NoExtendNativeOptions]{}},
		{"Object['toString'].bind = 0", rule.Given[NoExtendNativeOptions]{}},
		{"Object.defineProperty(x, 'p', {value: 0})", rule.Given[NoExtendNativeOptions]{}},
		{"Object.defineProperties(x, {p: {value: 0}})", rule.Given[NoExtendNativeOptions]{}},
		{"global.Object.prototype.toString = 0", rule.Given[NoExtendNativeOptions]{}},
		{"this.Object.prototype.toString = 0", rule.Given[NoExtendNativeOptions]{}},
		{"with(Object) { prototype.p = 0; }", rule.Given[NoExtendNativeOptions]{}},
		{"o = Object; o.prototype.toString = 0", rule.Given[NoExtendNativeOptions]{}},
		{"eval('Object.prototype.toString = 0')", rule.Given[NoExtendNativeOptions]{}},
		{"parseFloat.prototype.x = 1", rule.Given[NoExtendNativeOptions]{}},
		{"Object.prototype.g = 0", rule.Configured(NoExtendNativeOptions{Exceptions: []string{"Object"}})},
		{"obj[Object.prototype] = 0", rule.Given[NoExtendNativeOptions]{}},
		{"Object.defineProperty()", rule.Given[NoExtendNativeOptions]{}},
		{"Object.defineProperties()", rule.Given[NoExtendNativeOptions]{}},
		{"function foo() { var Object = function() {}; Object.prototype.p = 0 }", rule.Given[NoExtendNativeOptions]{}},
		{"{ let Object = function() {}; Object.prototype.p = 0 }", rule.Given[NoExtendNativeOptions]{}},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTypedGiven(test, NoExtendNative,
				extendNativeFile, testCase.sourceText, testCase.options))
		})
	}
}

// Where the finding points, which the message-id fixtures above cannot see.
//
// The two shapes point at different nodes and both are measured on eslint 10.8.1: an assignment
// reports the whole assignment, and a define call reports the whole call. A port reporting the
// `.prototype` member access instead passes every id fixture in this file.
func TestNoExtendNativeReportsTheWholeExtension(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText   string
		wantPosition int
		wantEnd      int
		builtin      string
	}{
		{"Object.prototype.p = 0", 0, 22, "Object"},
		{"Object.defineProperty(Array.prototype, 'p', {value: 0})", 0, 55, "Array"},
		{"Array.prototype.p &&= 0", 0, 23, "Array"},
		{"(Object?.prototype).p = 0", 0, 25, "Object"},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoExtendNative, extendNativeFile,
				testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one diagnostic, got %d", len(result.Diagnostics))
			}
			onDisk := strings.TrimSpace(testCase.sourceText) + "\n"
			got := result.Diagnostics[0].Range
			if got.Pos() != testCase.wantPosition || got.End() != testCase.wantEnd {
				test.Fatalf("reported [%d:%d) which is %q, wanted [%d:%d) which is %q",
					got.Pos(), got.End(), onDisk[got.Pos():got.End()],
					testCase.wantPosition, testCase.wantEnd, onDisk[testCase.wantPosition:testCase.wantEnd])
			}
		})
	}
}

// Silent shapes the corpus does not write, each measured upstream first.
//
// The first two are the ones that read like defects: `Object.prototype.p++` and
// `delete Object.prototype.p` both extend or change a native prototype and both are clean on eslint
// 10.8.1, because upstream matches an assignment expression and nothing else. Reproduced rather than
// improved on, and recorded here so the next reader does not helpfully "fix" it.
func TestNoExtendNativeDeclinesShapesTheCorpusOmits(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"an update expression rather than an assignment", "Object.prototype.p++"},
		{"a delete rather than an assignment", "delete Object.prototype.p"},
		{"an assignment to prototype itself", "Object.prototype = 0"},
		{"an assignment two levels below the prototype", "Object.prototype.p.q = 0"},
		{"a call that is not one of the two define methods", "Object.freeze(Array.prototype)"},
		{"a prototype in a later argument", "Object.defineProperty(x, Array.prototype)"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTyped(test, NoExtendNative,
				extendNativeFile, testCase.sourceText))
		})
	}
}

// The option surface, routed through the rule's own registered decoder rather than by building the
// struct here.
//
// Handing the harness a struct directly would leave the serde tag untested, and the tag is the line
// most likely to have no upstream counterpart. This decodes the same JSON the config would carry.
func TestNoExtendNativeDecodesItsOptions(test *testing.T) {
	test.Parallel()

	decode := rule.DecodeOptionsInto[NoExtendNativeOptions]()

	decoded, decodeError := decode(json.RawMessage(`{"exceptions": ["Object"]}`))
	if decodeError != nil {
		test.Fatalf("decoding failed: %v", decodeError)
	}
	if len(decoded.Exceptions) != 1 || decoded.Exceptions[0] != "Object" {
		test.Fatalf("exceptions decoded as %#v", decoded.Exceptions)
	}

	// And the decoded value reaches the rule: the same input reports without the exception.
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test, NoExtendNative, extendNativeFile,
		"Object.prototype.p = 0", decoded))
	if got := len(rule_testing.RunTyped(test, NoExtendNative, extendNativeFile,
		"Object.prototype.p = 0").Diagnostics); got != 1 {
		test.Fatalf("without the exception the same input gave %d diagnostics", got)
	}
}

// A rule configured as a bare "error" is handed no options, which must land on the strict default.
//
// This bypasses the decoder entirely, which is the path a bare `"error"` actually takes: the config
// runs the rule unconfigured, and reading unconfigured options yields the zero value. The zero value
// being correct here is a coincidence worth pinning rather than relying on.
func TestNoExtendNativeDefaultsWithoutTheDecoder(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectFindings(test, rule_testing.RunTyped(test, NoExtendNative, extendNativeFile,
		"Object.prototype.p = 0"), "unexpected")
}

// The typed harness is required, and a revert to the plain one must fail loudly.
//
// The rule guards on a nil checker, so under `rule_testing.Run` it goes silent rather than panicking,
// and every StaysSilent case above would pass vacuously.
func TestNoExtendNativeNeedsTheTypedHarness(test *testing.T) {
	test.Parallel()

	const source = "Object.prototype.p = 0"

	rule_testing.ExpectFindings(test, rule_testing.RunTyped(test, NoExtendNative, extendNativeFile,
		source), "unexpected")

	if got := len(rule_testing.Run(test, NoExtendNative, extendNativeFile,
		source).Diagnostics); got != 0 {
		test.Fatalf("the untyped harness produced %d diagnostics, so the nil-checker guard has moved "+
			"and this test no longer measures what it claims", got)
	}
}

// Reading a native prototype property is not extending it, which the imported corpus cannot see.
//
// The corpus writes 21 reporting cases and every one of them is an assignment or a define call, so a
// port that anchors on `KindBinaryExpression` and forgets to ask which operator it found passes all
// 40 imported cases. Removing the operator guard survived them, which is how this test came to
// exist.
//
// Reading is legitimate and common: comparing, adding, or testing a native prototype property does
// not change the shared object. All five rows are clean on eslint 10.8.1, measured before they were
// written here.
func TestNoExtendNativeDeclinesReadingAPrototypeProperty(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"a strict comparison", "Object.prototype.p === 0"},
		{"an addition", "Object.prototype.p + 0"},
		{"a comma expression", "Object.prototype.p, 0"},
		{"a comparison in a condition", "if (Object.prototype.p == 0) {}"},
		{"an instanceof", "Object.prototype.p instanceof Foo"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTyped(test, NoExtendNative,
				extendNativeFile, testCase.sourceText))
		})
	}
}

// The define call's receiver must be `Object`, which the imported corpus cannot see.
//
// Every define case in the corpus writes `Object.defineProperty` or `Object.defineProperties`, so a
// port that checks the method name and forgets the receiver passes all 40 imported cases. A mutation
// dropping exactly that check survived them.
//
// `Reflect.defineProperty` is the sharp row: it is a real API that extends the prototype in exactly
// the same way, and eslint 10.8.1 is silent on it because upstream matches the receiver name
// `Object` and nothing else. That is a genuine gap in upstream's coverage, reproduced here rather
// than improved on, and recorded so the next reader does not helpfully widen it.
//
// All four measured clean on eslint 10.8.1 before being written here.
func TestNoExtendNativeDeclinesADefineCallOnAnotherReceiver(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{
			"Reflect.defineProperty, which really does the same thing",
			"Reflect.defineProperty(Array.prototype, \"p\", {value:0})",
		},
		{"an unrelated object's defineProperty", "Foo.defineProperty(Array.prototype, \"p\", {value:0})"},
		{"another builtin's defineProperty", "Math.defineProperty(Array.prototype, \"p\", {value:0})"},
		{"another builtin's defineProperties", "JSON.defineProperties(Array.prototype, {})"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTyped(test, NoExtendNative,
				extendNativeFile, testCase.sourceText))
		})
	}
}

// A no-substitution template subscript names the property too, which the corpus cannot see.
//
// The corpus writes `String['prototype']` and `Number['prototype']['p']`, so it exercises the string
// subscript but never the template one, and the narrower `property.Textual` accept set covers
// strings. A mutation narrowing the set therefore survived all 40 imported cases.
//
// Upstream asks for the static property name, which resolves a no-substitution template as well, and
// eslint 10.8.1 reports all three rows below. They cover the three positions the rule reads a
// property name in: the prototype of an assignment target, the prototype inside a define call, and
// the define method's own name.
func TestNoExtendNativeReadsATemplateSubscript(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		builtin    string
	}{
		{"a template subscript for prototype", "Object[`prototype`].p = 0", "Object"},
		{
			"a template prototype inside a define call",
			"Object.defineProperty(Array[`prototype`], \"p\", {value:0})",
			"Array",
		},
		{
			"a template subscript for the define method",
			"Object[`defineProperty`](Array.prototype, \"p\", {value:0})",
			"Array",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoExtendNative, extendNativeFile,
				testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one diagnostic, got %d", len(result.Diagnostics))
			}
			if want := "`" + testCase.builtin + ".prototype`"; !strings.Contains(
				result.Diagnostics[0].Message.Description, want) {
				test.Fatalf("did not name %s: %q", want, result.Diagnostics[0].Message.Description)
			}
		})
	}
}
