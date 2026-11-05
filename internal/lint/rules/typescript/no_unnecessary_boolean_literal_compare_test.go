package typescript

import (
	"strconv"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/testing"
)

const noUnnecessaryBooleanLiteralCompareFile = "/repository/source/Thing.ts"

// noUnnecessaryBooleanLiteralCompareCaseName numbers a row so a failure names which one, since many
// rows differ only in an operator or in the option above them.
func noUnnecessaryBooleanLiteralCompareCaseName(index int) string {
	return "case" + strconv.Itoa(index)
}

// noUnnecessaryBooleanLiteralCompareOnDisk is what the harness actually writes for a fixture.
//
// `RunTyped` writes each file as `strings.TrimSpace(contents)+"\n"`, so a case copied from an
// upstream tester carries a leading newline the file on disk does not have. A span sliced from the
// Go literal is therefore off by one, and an expected fix output compared against the untrimmed
// literal fails on a trailing newline while the repair is byte correct.
func noUnnecessaryBooleanLiteralCompareOnDisk(sourceText string) string {
	return strings.TrimSpace(sourceText) + "\n"
}

// decodeNoUnnecessaryBooleanLiteralCompareOptions runs a configuration through the real decoder.
//
// Two of this rule's three options default to true, so a fixture building the options struct by hand
// would leave the one line most likely to be wrong untested: a zero-valued struct turns both
// allowances off and makes the rule report two families upstream allows. An empty configuration is
// the bare `"error"` case, which the config layer turns into nil options.
func decodeNoUnnecessaryBooleanLiteralCompareOptions(test *testing.T, configuration string) any {
	test.Helper()

	if configuration == "" {
		// A rule configured as a bare `"error"` is handed nil, not an empty struct. Passing nil
		// here is what puts the rule's own fallback under test rather than the decoder's.
		return nil
	}
	options, decodeNoUnnecessaryBooleanLiteralCompareOptionsError := DecodeNoUnnecessaryBooleanLiteralCompareOptions([]byte(configuration))
	if decodeNoUnnecessaryBooleanLiteralCompareOptionsError != nil {
		test.Fatalf("could not decode %s: %v", configuration, decodeNoUnnecessaryBooleanLiteralCompareOptionsError)
	}
	return options
}

// TestNoUnnecessaryBooleanLiteralCompareStaysSilentOnUpstreamPassCases is the imported clean corpus.
//
// Twenty-one of upstream's twenty-two passing inputs, extracted from the clone's test file by
// parsing it with the TypeScript compiler rather than by reading it, so no escape sequence passed
// through a shell or a keyboard on the way here. Every one was additionally run through the
// installed 8.67.0 build over a real program, which reported nothing on all twenty-one.
//
// The twenty-second is omitted deliberately and pinned elsewhere: it is silent upstream only because
// of an `eslint-disable-next-line` comment, and `rule_testing` never consults the suppression layer.
// Recording it as clean here would assert a property of a layer this test cannot reach. See
// `TestNoUnnecessaryBooleanLiteralCompareSuppressionCaseIsDecidedAboveTheRule`.
func TestNoUnnecessaryBooleanLiteralCompareStaysSilentOnUpstreamPassCases(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
	}{
		{
			configuration: "",
			sourceText:    "\ndeclare const varAny: any;\nvarAny === true;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varAny: any;\nvarAny == false;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varString: string;\nvarString === false;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varString: string;\nvarString === true;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varObject: {};\nvarObject === true;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varObject: {};\nvarObject == false;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varNullOrUndefined: null | undefined;\nvarNullOrUndefined === false;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBooleanOrString: boolean | string;\nvarBooleanOrString === false;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBooleanOrString: boolean | string;\nvarBooleanOrString == true;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varTrueOrStringOrUndefined: true | string | undefined;\nvarTrueOrStringOrUndefined == true;\n    ",
		},
		{
			configuration: "",
			sourceText:    "\nconst test: <T>(someCondition: T) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n    ",
		},
		{
			configuration: "",
			sourceText:    "\nconst test: <T>(someCondition: boolean | string) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n    ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBooleanOrUndefined: boolean | undefined;\nvarBooleanOrUndefined === true;\n    ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\ndeclare const varBooleanOrUndefined: boolean | undefined;\nvarBooleanOrUndefined === true;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const varBooleanOrUndefined: boolean | undefined;\nvarBooleanOrUndefined === false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\nconst test: <T extends boolean | undefined>(\n  someCondition: T,\n) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\nconst test: <T extends boolean | undefined>(\n  someCondition: T,\n) => void = someCondition => {\n  if (someCondition === false) {\n  }\n};\n      ",
		},
		{
			configuration: "",
			sourceText:    "'false' === true;",
		},
		{
			configuration: "",
			sourceText:    "'true' === false;",
		},
		{
			configuration: "",
			sourceText:    "\nconst unconstrained: <T>(someCondition: T) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n    ",
		},
		{
			configuration: "",
			sourceText:    "\nconst extendsUnknown: <T extends unknown>(\n  someCondition: T,\n) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n    ",
		},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test,
				NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
				testCase.sourceText,
				decodeNoUnnecessaryBooleanLiteralCompareOptions(test, testCase.configuration)))
		})
	}
}

// TestNoUnnecessaryBooleanLiteralCompareFiresOnUpstreamFailCases is the imported reporting corpus.
//
// Forty-nine of upstream's fifty reporting inputs, each carrying one finding and a repair. The
// fiftieth needs a project with `strictNullChecks` off, which this harness cannot produce, and is
// pinned by a unit test on the option resolution instead.
//
// Three things are asserted per row and each catches a different defect. The ids say which of the
// five arms ran, and they differ only in wording. The spans say where the finding points, which is
// the whole comparison rather than the compared expression. And the applied source says what the
// edit engine will write unattended, which no message-id assertion can see.
func TestNoUnnecessaryBooleanLiteralCompareFiresOnUpstreamFailCases(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantIds       []string
		wantSpans     []string
		wantFixed     string
	}{
		{
			configuration: "",
			sourceText:    "true === true;",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"true === true"},
			wantFixed:     "true;",
		},
		{
			configuration: "",
			sourceText:    "false !== true;",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"false !== true"},
			wantFixed:     "!false;",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (varBoolean !== false) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"varBoolean !== false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (varBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varTrue: true;\nif (varTrue !== true) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"varTrue !== true"},
			wantFixed:     "\ndeclare const varTrue: true;\nif (!varTrue) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const varTrueOrUndefined: true | undefined;\nif (varTrueOrUndefined === true) {\n}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"varTrueOrUndefined === true"},
			wantFixed:     "\ndeclare const varTrueOrUndefined: true | undefined;\nif (varTrueOrUndefined) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const varFalseOrNull: false | null;\nif (varFalseOrNull !== true) {\n}\n      ",
			wantIds:       []string{"comparingNullableToTrueNegated"},
			wantSpans:     []string{"varFalseOrNull !== true"},
			wantFixed:     "\ndeclare const varFalseOrNull: false | null;\nif (!varFalseOrNull) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\nconst isTrue = (x: boolean | undefined): boolean => x === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\nconst isTrue = (x: boolean | undefined): boolean => x ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\nfunction isTrue(x: boolean | undefined): boolean {\n  return x === true;\n}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\nfunction isTrue(x: boolean | undefined): boolean {\n  return x ?? false;\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\nconst value: boolean = x === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\nconst value: boolean = x ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare function acceptsBoolean(value: boolean): void;\n\nacceptsBoolean(x === true);\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare function acceptsBoolean(value: boolean): void;\n\nacceptsBoolean(x ?? false);\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nif (x === true) {\n}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nif (x) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nwhile (x === true) {}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nwhile (x) {}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\ndo {} while (x === true);\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\ndo {} while (x);\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nfor (; x === true;) {}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nfor (; x;) {}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value = x === true ? 'true' : 'false';\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value = x ? 'true' : 'false';\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const condition: boolean;\ndeclare const x: boolean | undefined;\n\nconst value = condition ? x === true : false;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const condition: boolean;\ndeclare const x: boolean | undefined;\n\nconst value = condition ? (x ?? false) : false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nif (other && x === true) {\n}\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nif (other && x) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = other && x === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = other && (x ?? false);\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = (x && other) === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"(x && other) === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = (x && other) ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = (x && other) === false;\n      ",
			wantIds:       []string{"comparingNullableToFalse"},
			wantSpans:     []string{"(x && other) === false"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare const other: boolean;\n\nconst value: boolean = !((x && other) ?? true);\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = true === x;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"true === x"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = x ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = x !== true;\n      ",
			wantIds:       []string{"comparingNullableToTrueNegated"},
			wantSpans:     []string{"x !== true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = !x;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = x != true;\n      ",
			wantIds:       []string{"comparingNullableToTrueNegated"},
			wantSpans:     []string{"x != true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = !x;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = !(x === true);\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = !x;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = !(x !== true);\n      ",
			wantIds:       []string{"comparingNullableToTrueNegated"},
			wantSpans:     []string{"x !== true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\n\nconst value: boolean = x ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\ndeclare const varBooleanOrNull: boolean | null;\ndeclare const otherBoolean: boolean;\nif (varBooleanOrNull === false && otherBoolean) {\n}\n      ",
			wantIds:       []string{"comparingNullableToFalse"},
			wantSpans:     []string{"varBooleanOrNull === false"},
			wantFixed:     "\ndeclare const varBooleanOrNull: boolean | null;\ndeclare const otherBoolean: boolean;\nif (!(varBooleanOrNull ?? true) && otherBoolean) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\ndeclare const varBooleanOrNull: boolean | null;\ndeclare const otherBoolean: boolean;\nif (!(varBooleanOrNull === false) || otherBoolean) {\n}\n      ",
			wantIds:       []string{"comparingNullableToFalse"},
			wantSpans:     []string{"varBooleanOrNull === false"},
			wantFixed:     "\ndeclare const varBooleanOrNull: boolean | null;\ndeclare const otherBoolean: boolean;\nif ((varBooleanOrNull ?? true) || otherBoolean) {\n}\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToFalse\":false}",
			sourceText:    "\ndeclare const varTrueOrFalseOrUndefined: true | false | undefined;\ndeclare const otherBoolean: boolean;\nif (varTrueOrFalseOrUndefined !== false && !otherBoolean) {\n}\n      ",
			wantIds:       []string{"comparingNullableToFalse"},
			wantSpans:     []string{"varTrueOrFalseOrUndefined !== false"},
			wantFixed:     "\ndeclare const varTrueOrFalseOrUndefined: true | false | undefined;\ndeclare const otherBoolean: boolean;\nif ((varTrueOrFalseOrUndefined ?? true) && !otherBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (false !== varBoolean) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"false !== varBoolean"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (varBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (true !== varBoolean) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"true !== varBoolean"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (!varBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\n        declare const x;\n        if ((x instanceof Error) === false) {\n        }\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"(x instanceof Error) === false"},
			wantFixed:     "\n        declare const x;\n        if (!(x instanceof Error)) {\n        }\n      ",
		},
		{
			configuration: "",
			sourceText:    "\n        declare const x;\n        if (false === (x instanceof Error)) {\n        }\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"false === (x instanceof Error)"},
			wantFixed:     "\n        declare const x;\n        if (!(x instanceof Error)) {\n        }\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const x;\nif (x instanceof Error === false) {\n}\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"x instanceof Error === false"},
			wantFixed:     "\ndeclare const x;\nif (!(x instanceof Error)) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\n        declare const x;\n        if (typeof x === 'string' === false) {\n        }\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"typeof x === 'string' === false"},
			wantFixed:     "\n        declare const x;\n        if (!(typeof x === 'string')) {\n        }\n      ",
		},
		{
			configuration: "",
			sourceText:    "\n        declare const x;\n        if (x instanceof Error === (false)) {\n        }\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"x instanceof Error === (false)"},
			wantFixed:     "\n        declare const x;\n        if (!(x instanceof Error)) {\n        }\n      ",
		},
		{
			configuration: "",
			sourceText:    "\n        declare const x;\n        if ((false) === x instanceof Error) {\n        }\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"(false) === x instanceof Error"},
			wantFixed:     "\n        declare const x;\n        if (!(x instanceof Error)) {\n        }\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!(varBoolean !== false)) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"varBoolean !== false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (!varBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!(varBoolean === false)) {\n}\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"varBoolean === false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (varBoolean) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!(varBoolean instanceof Event == false)) {\n}\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"varBoolean instanceof Event == false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (varBoolean instanceof Event) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (varBoolean instanceof Event == false) {\n}\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"varBoolean instanceof Event == false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (!(varBoolean instanceof Event)) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!((varBoolean ?? false) !== false)) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"(varBoolean ?? false) !== false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (!(varBoolean ?? false)) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!((varBoolean ?? false) === false)) {\n}\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"(varBoolean ?? false) === false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (varBoolean ?? false) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const varBoolean: boolean;\nif (!((varBoolean ?? true) !== false)) {\n}\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"(varBoolean ?? true) !== false"},
			wantFixed:     "\ndeclare const varBoolean: boolean;\nif (!(varBoolean ?? true)) {\n}\n      ",
		},
		{
			configuration: "",
			sourceText:    "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (someCondition === true) {\n  }\n};\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"someCondition === true"},
			wantFixed:     "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (someCondition) {\n  }\n};\n      ",
		},
		{
			configuration: "",
			sourceText:    "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (!(someCondition !== false)) {\n  }\n};\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"someCondition !== false"},
			wantFixed:     "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (!someCondition) {\n  }\n};\n      ",
		},
		{
			configuration: "",
			sourceText:    "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (!((someCondition ?? true) !== false)) {\n  }\n};\n      ",
			wantIds:       []string{"negated"},
			wantSpans:     []string{"(someCondition ?? true) !== false"},
			wantFixed:     "\nconst test: <T extends boolean>(someCondition: T) => void = someCondition => {\n  if (!(someCondition ?? true)) {\n  }\n};\n      ",
		},
		{
			configuration: "",
			sourceText:    "\ndeclare const a: boolean;\ndeclare const b: boolean;\ndeclare const c: boolean;\n(a || b) === true && c;\n      ",
			wantIds:       []string{"direct"},
			wantSpans:     []string{"(a || b) === true"},
			wantFixed:     "\ndeclare const a: boolean;\ndeclare const b: boolean;\ndeclare const c: boolean;\n(a || b) && c;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\nx === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"x === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\nx ?? false;\n      ",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\":false}",
			sourceText:    "\ndeclare const x: boolean | undefined;\ndeclare const y: boolean | undefined;\n(x || y) === true;\n      ",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantSpans:     []string{"(x || y) === true"},
			wantFixed:     "\ndeclare const x: boolean | undefined;\ndeclare const y: boolean | undefined;\n(x || y) ?? false;\n      ",
		},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnnecessaryBooleanLiteralCompare,
				noUnnecessaryBooleanLiteralCompareFile, testCase.sourceText,
				decodeNoUnnecessaryBooleanLiteralCompareOptions(test, testCase.configuration))
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)

			onDisk := noUnnecessaryBooleanLiteralCompareOnDisk(testCase.sourceText)
			for index, wantSpan := range testCase.wantSpans {
				reported := onDisk[result.Diagnostics[index].Range.Pos():result.Diagnostics[index].Range.End()]
				if reported != wantSpan {
					test.Errorf("finding %d covers %q, want %q", index, reported, wantSpan)
				}
			}

			if testCase.wantFixed == "" {
				for index, diagnostic := range result.Diagnostics {
					if len(diagnostic.Fixes) != 0 {
						test.Errorf("finding %d carries %d fixes, want none", index, len(diagnostic.Fixes))
					}
				}
				return
			}
			rule_testing.ExpectFixedSource(test, result,
				noUnnecessaryBooleanLiteralCompareOnDisk(testCase.wantFixed))
		})
	}
}

// TestNoUnnecessaryBooleanLiteralCompareKeepsEverythingInTheExpression is the type-safety fixture.
//
// This rule's repair replaces a comparison with the compared expression, and that is the shape which
// lost type information twice in this project: one rewrite stranded a type annotation and widened
// eight declarations, another dropped a return annotation it could not see.
//
// It is safe here for a structural reason rather than a careful one: the replacement is built from
// the expression's own source text, so nothing inside it is re-rendered and nothing inside it can be
// lost. The span being replaced holds only the comparison operator and the boolean literal.
//
// These rows are what proves it. Upstream's corpus is TypeScript and does carry some of these, but
// not the generic call with two arguments, not the `satisfies`, and not the comment, and those are
// exactly the shapes a re-rendering fixer would drop while every other fixture stayed green. Every
// expectation is what the installed 8.67.0 build produced.
func TestNoUnnecessaryBooleanLiteralCompareKeepsEverythingInTheExpression(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		wantFixed  string
		reason     string
	}{
		{
			sourceText: "declare function f<T>(): boolean;\nconst z = f<string>() === true;",
			wantFixed:  "declare function f<T>(): boolean;\nconst z = f<string>();",
			reason:     "a generic call keeps its type arguments",
		},
		{
			sourceText: "declare const x: unknown;\nconst z = ((x as boolean)) === true;",
			wantFixed:  "declare const x: unknown;\nconst z = x as boolean;",
			reason:     "a type assertion survives",
		},
		{
			sourceText: "declare const b: boolean;\nconst z = (b satisfies boolean) === true;",
			wantFixed:  "declare const b: boolean;\nconst z = b satisfies boolean;",
			reason:     "a satisfies expression survives",
		},
		{
			sourceText: "declare const b: boolean | undefined;\nconst z = b! === true;",
			wantFixed:  "declare const b: boolean | undefined;\nconst z = b!;",
			reason:     "a non-null assertion survives",
		},
		{
			sourceText: "declare const o: {b: boolean};\nconst z = o[`b`] === true;",
			wantFixed:  "declare const o: {b: boolean};\nconst z = o[`b`];",
			reason:     "a template element access survives",
		},
		{
			sourceText: "declare const b: boolean;\nconst z = /* c */ b === true;",
			wantFixed:  "declare const b: boolean;\nconst z = /* c */ b;",
			reason:     "a comment before the expression survives",
		},
		{
			sourceText: "declare function g<A,B>(a:A,b:B): boolean;\nconst z = g<string,number>(\"a\",1) === true;",
			wantFixed:  "declare function g<A,B>(a:A,b:B): boolean;\nconst z = g<string,number>(\"a\",1);",
			reason:     "two type arguments survive",
		},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnnecessaryBooleanLiteralCompare,
				noUnnecessaryBooleanLiteralCompareFile, testCase.sourceText, nil)
			rule_testing.ExpectFindings(test, result, "direct")
			rule_testing.ExpectFixedSource(test, result,
				noUnnecessaryBooleanLiteralCompareOnDisk(testCase.wantFixed))
		})
	}
}

// TestNoUnnecessaryBooleanLiteralCompareDiscriminatesOnCasesUpstreamDoesNotWrite covers the rest.
//
// Upstream's corpus writes no parenthesized comparison at all, which matters here and cannot matter
// there: TSESTree deletes the node, so its parser hands upstream what is inside and ours hands the
// wrapper. Four rows cover that, and the remaining rows cover the type discriminations the corpus
// touches only in passing.
//
// Each row was run through the installed 8.67.0 build and carries the verdict that build produced,
// so a row asserting silence asserts upstream's silence rather than this port's.
func TestNoUnnecessaryBooleanLiteralCompareDiscriminatesOnCasesUpstreamDoesNotWrite(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		sourceText    string
		wantIds       []string
		wantFixed     string
		reason        string
	}{
		{
			configuration: "",
			sourceText:    "declare const a: boolean;\ndeclare const c: boolean;\ndeclare const y: boolean;\nconst z = (a || c) === true && y;",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const a: boolean;\ndeclare const c: boolean;\ndeclare const y: boolean;\nconst z = (a || c) && y;",
			reason:        "a parenthesized binary needs its parentheses back",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\ndeclare const y: boolean;\nconst z = (b) === true && y;",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const b: boolean;\ndeclare const y: boolean;\nconst z = b && y;",
			reason:        "a parenthesized identifier does not",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\nif ((b === true)) {}",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const b: boolean;\nif ((b)) {}",
			reason:        "a parenthesized comparison is still a conditional test",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\nconst z = ((b)) === true;",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const b: boolean;\nconst z = b;",
			reason:        "nested parentheses unwrap all the way",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean | string;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a union with a non-boolean survivor is not this rule",
		},
		{
			configuration: "",
			sourceText:    "declare const b: null | undefined;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a wholly nullish union has no non-nullish part",
		},
		{
			// The same three shapes again with both allowances turned off, which is what makes the
			// union test decide them rather than the allowance check above it.
			//
			// With the default options every nullable comparison is allowed, so a mutant breaking
			// the union test still produced silence and survived the whole suite. The rows above
			// cover the shape and mask the guard; these cover the guard. Measured: with the
			// allowances off, `null | undefined` reports under the broken version and is silent
			// under the correct one.
			configuration: "{\"allowComparingNullableBooleansToTrue\": false, \"allowComparingNullableBooleansToFalse\": false}",
			sourceText:    "declare const b: null | undefined;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a wholly nullish union is declined by the union test, not by an allowance",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\": false, \"allowComparingNullableBooleansToFalse\": false}",
			sourceText:    "declare const b: boolean | 0;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a non-nullable union is declined by the union test",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\": false, \"allowComparingNullableBooleansToFalse\": false}",
			sourceText:    "declare const b: boolean | string | undefined;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a nullable union with a non-boolean survivor is declined too",
		},
		{
			configuration: "{\"allowComparingNullableBooleansToTrue\": false, \"allowComparingNullableBooleansToFalse\": false}",
			sourceText:    "function f<T>(b: T) { return b === true; }",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "an unconstrained type parameter declines with the allowances off too",
		},
		{
			// The control for the four rows above: with the allowances off, a genuine nullable
			// boolean does report, so their silence is the union test rather than the options.
			configuration: "{\"allowComparingNullableBooleansToTrue\": false, \"allowComparingNullableBooleansToFalse\": false}",
			sourceText:    "declare const b: boolean | undefined;\nconst z = b === true;",
			wantIds:       []string{"comparingNullableToTrueDirect"},
			wantFixed:     "declare const b: boolean | undefined;\nconst z = b ?? false;",
			reason:        "the control: a real nullable boolean reports once the allowance is off",
		},
		{
			configuration: "",
			sourceText:    "function f<T>(b: T) { return b === true; }",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "an unconstrained type parameter declines",
		},
		{
			configuration: "",
			sourceText:    "function f<T extends boolean>(b: T) { return b === true; }",
			wantIds:       []string{"direct"},
			wantFixed:     "function f<T extends boolean>(b: T) { return b; }",
			reason:        "a constrained one is judged by its constraint",
		},
		{
			configuration: "",
			sourceText:    "declare const b: any;\nconst z = b === true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "any is not a boolean to this rule",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\nconst z = true === b;",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const b: boolean;\nconst z = b;",
			reason:        "the literal may be on the left",
		},
		{
			configuration: "",
			sourceText:    "const z = true === true;",
			wantIds:       []string{"direct"},
			wantFixed:     "const z = true;",
			reason:        "two literals: the right is taken as the literal",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\nconst z = b == true;",
			wantIds:       []string{"direct"},
			wantFixed:     "declare const b: boolean;\nconst z = b;",
			reason:        "loose equality reports too",
		},
		{
			configuration: "",
			sourceText:    "declare const b: boolean;\nconst z = b >= true;",
			wantIds:       nil,
			wantFixed:     "",
			reason:        "a non-equality operator is not this rule",
		},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(test, NoUnnecessaryBooleanLiteralCompare,
				noUnnecessaryBooleanLiteralCompareFile, testCase.sourceText,
				decodeNoUnnecessaryBooleanLiteralCompareOptions(test, testCase.configuration))
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
			if testCase.wantFixed != "" {
				rule_testing.ExpectFixedSource(test, result,
					noUnnecessaryBooleanLiteralCompareOnDisk(testCase.wantFixed))
			}
		})
	}
}

// TestNoUnnecessaryBooleanLiteralCompareDecoderKeepsTheDefaultsTrue is the option trap.
//
// Two of the three options default to true. A generic decode over a non-pointer struct yields false
// for every absent key, which turns both allowances off and makes the rule report two families of
// comparison upstream allows by default. That inversion is invisible to any fixture that builds the
// options struct directly, which is why every other test in this file routes through the decoder and
// why this one asserts the decoder's own output.
func TestNoUnnecessaryBooleanLiteralCompareDecoderKeepsTheDefaultsTrue(test *testing.T) {
	test.Parallel()

	cases := []struct {
		configuration string
		wantFalse     bool
		wantTrue      bool
		wantUnstrict  bool
	}{
		{"{}", true, true, false},
		{"{\"allowComparingNullableBooleansToFalse\": false}", false, true, false},
		{"{\"allowComparingNullableBooleansToTrue\": false}", true, false, false},
		{"{\"allowComparingNullableBooleansToFalse\": false, \"allowComparingNullableBooleansToTrue\": false}", false, false, false},
		{"{\"allowRuleToRunWithoutStrictNullChecksIKnowWhatIAmDoing\": true}", true, true, true},
		// An explicit true is not distinguishable from the default in behavior, but it is what the
		// pointer field exists to allow, so it is asserted rather than assumed.
		{"{\"allowComparingNullableBooleansToFalse\": true}", true, true, false},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			decoded, decodeNoUnnecessaryBooleanLiteralCompareOptionsError := DecodeNoUnnecessaryBooleanLiteralCompareOptions([]byte(testCase.configuration))
			if decodeNoUnnecessaryBooleanLiteralCompareOptionsError != nil {
				test.Fatalf("decoding %s: %v", testCase.configuration, decodeNoUnnecessaryBooleanLiteralCompareOptionsError)
			}
			options, ok := decoded.(NoUnnecessaryBooleanLiteralCompareOptions)
			if !ok {
				test.Fatalf("the decoder returned %T rather than the options struct", decoded)
			}
			if options.AllowComparingNullableBooleansToFalse != testCase.wantFalse {
				test.Errorf("allowComparingNullableBooleansToFalse resolved to %v, want %v",
					options.AllowComparingNullableBooleansToFalse, testCase.wantFalse)
			}
			if options.AllowComparingNullableBooleansToTrue != testCase.wantTrue {
				test.Errorf("allowComparingNullableBooleansToTrue resolved to %v, want %v",
					options.AllowComparingNullableBooleansToTrue, testCase.wantTrue)
			}
			if options.AllowRuleToRunWithoutStrictNullChecks != testCase.wantUnstrict {
				test.Errorf("the strictNullChecks escape resolved to %v, want %v",
					options.AllowRuleToRunWithoutStrictNullChecks, testCase.wantUnstrict)
			}
		})
	}
}

// TestNoUnnecessaryBooleanLiteralCompareNilOptionsAllowsTheNullishFamilies bypasses the decoder.
//
// A rule configured as a bare `"error"` is handed nil, and a bare type assertion on nil yields the
// zero value, which for this rule turns both allowances off rather than merely silencing the rule.
// That is worse than the usual inert shape: the rule would report where upstream is silent, and
// every decoder-routed fixture would stay green.
func TestNoUnnecessaryBooleanLiteralCompareNilOptionsAllowsTheNullishFamilies(test *testing.T) {
	test.Parallel()

	// A plain boolean still reports on nil options.
	rule_testing.ExpectFindings(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		"declare const b: boolean;\nconst z = b === true;", nil), "direct")

	// Both nullish families are allowed by default, so these must be silent. A zero-valued options
	// struct would report both.
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		"declare const b: boolean | undefined;\nconst z = b === true;", nil))
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		"declare const b: boolean | undefined;\nconst z = b === false;", nil))
}

// TestNoUnnecessaryBooleanLiteralCompareRequiresTheTypedHarness pins the checker declaration.
//
// A typed rule handed the plain harness gets a nil checker, and the guard at the top of the listener
// turns that into silence rather than a panic. Silence is the more dangerous failure: every clean
// case passes vacuously and every reporting case fails in a way that reads as a rule bug.
func TestNoUnnecessaryBooleanLiteralCompareRequiresTheTypedHarness(test *testing.T) {
	test.Parallel()

	source := "declare const b: boolean;\nconst z = b === true;"
	rule_testing.ExpectClean(test, rule_testing.Run(test, NoUnnecessaryBooleanLiteralCompare,
		noUnnecessaryBooleanLiteralCompareFile, source))
	rule_testing.ExpectFindings(test, rule_testing.RunTyped(test, NoUnnecessaryBooleanLiteralCompare,
		noUnnecessaryBooleanLiteralCompareFile, source), "direct")
}

// TestNoUnnecessaryBooleanLiteralCompareStrictNullChecksArmIsHarnessBlocked records what cannot be
// fixtured, and pins the predicate that decides it instead.
//
// Upstream reports once per file when `strictNullChecks` is off and the escape option is not set.
// That arm is reproduced in the rule and it cannot be reached through `rule_testing`: the harness
// writes its own tsconfig after the setup hook runs, so a fixture cannot turn the option off.
//
// Probed directly rather than assumed. A hook writing `strict:false` and `strictNullChecks:false`
// into the fixture directory produced a program whose resolved option was still true, identically to
// the default path. So this is the brief's unreachable-through-the-harness category, and the honest
// response is to pin the predicate rather than weaken the rule to make a fixture green.
//
// What is asserted here is the resolution `GetStrictOptionValue` performs, which is the whole of the
// decision: an explicit value wins, an unset one falls back to `strict`, and `strict: false` with no
// explicit value resolves to off.
func TestNoUnnecessaryBooleanLiteralCompareStrictNullChecksArmIsHarnessBlocked(test *testing.T) {
	test.Parallel()

	cases := []struct {
		strict           core.Tristate
		strictNullChecks core.Tristate
		want             bool
		reason           string
	}{
		{core.TSTrue, core.TSUnknown, true, "strict on, unset falls back to it"},
		{core.TSFalse, core.TSUnknown, false, "strict off, unset falls back to it"},
		{core.TSUnknown, core.TSUnknown, true, "neither set resolves to on"},
		{core.TSFalse, core.TSTrue, true, "an explicit true wins over strict off"},
		{core.TSTrue, core.TSFalse, false, "an explicit false wins over strict on"},
	}
	for index, testCase := range cases {
		test.Run(noUnnecessaryBooleanLiteralCompareCaseName(index), func(test *testing.T) {
			test.Parallel()
			options := core.CompilerOptions{
				Strict:           testCase.strict,
				StrictNullChecks: testCase.strictNullChecks,
			}
			if got := options.GetStrictOptionValue(options.StrictNullChecks); got != testCase.want {
				test.Errorf("resolved to %v, want %v (%s)", got, testCase.want, testCase.reason)
			}
		})
	}

	// The harness cannot produce a program with the option off, which is why the rows above test the
	// resolution rather than the rule. Asserted so the claim is a measurement rather than a comment:
	// under the harness the rule is silent on this arm, because the option always resolves on.
	rule_testing.ExpectClean(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		"function foo(): boolean { return true; }",
		decodeNoUnnecessaryBooleanLiteralCompareOptions(test,
			"{\"allowRuleToRunWithoutStrictNullChecksIKnowWhatIAmDoing\": false}")))
}

// TestNoUnnecessaryBooleanLiteralCompareSuppressionCaseIsDecidedAboveTheRule records the other
// case the harness cannot reproduce.
//
// Upstream ships this as a passing case and it is silent there only because of the
// `eslint-disable-next-line` comment inside it. `rule_testing.Run` never consults
// `internal/suppression`, so no rule test can reproduce that silence, and recording it in the clean
// list would assert a property of a layer this test cannot reach.
//
// So it is pinned at the layer that actually decides it: the rule reports, and the suppression layer
// is what makes it clean in a real run. Confirmed by removing the comment and watching the same
// source report identically.
func TestNoUnnecessaryBooleanLiteralCompareSuppressionCaseIsDecidedAboveTheRule(test *testing.T) {
	test.Parallel()

	withComment := "function test(a?: boolean): boolean {\n  // eslint-disable-next-line\n  return a !== false;\n}"
	withoutComment := "function test(a?: boolean): boolean {\n  return a !== false;\n}"

	options := decodeNoUnnecessaryBooleanLiteralCompareOptions(test,
		"{\"allowComparingNullableBooleansToFalse\": false}")

	rule_testing.ExpectFindings(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		withComment, options), "comparingNullableToFalse")
	rule_testing.ExpectFindings(test, rule_testing.RunTypedWithOptions(test,
		NoUnnecessaryBooleanLiteralCompare, noUnnecessaryBooleanLiteralCompareFile,
		withoutComment, options), "comparingNullableToFalse")
}
