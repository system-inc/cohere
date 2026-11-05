package react

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// forbidElementsFile is where the fixtures pretend to live.
//
// A .tsx extension because most cases hold JSX. The rule has no suffix gate, which a case below
// pins by writing reporting source to a plain `.ts` file.
const forbidElementsFile = "/repository/source/ForbidElements.tsx"

// The corpus is upstream's, extracted mechanically rather than retyped.
//
// Every case in the two tables below comes from
// /tmp/lint-sources/eslint-plugin-react/tests/lib/rules/forbid-elements.js, read by evaluating the
// two arrays in the tester with a stubbed RuleTester and emitting Go raw strings from the decoded
// values, so no escape sequence was typed on the way here. The option bodies are the decoded
// objects re-serialised, which is what the config layer would hand the decoder. Upstream carries 14
// valid and 16 invalid cases, and the invalid ones name 22 findings between them.
//
// All 30 were replayed against the installed build, eslint-plugin-react 7.37.5, through the ESLint
// Linter API before any Go was written, and the corpus and the running rule agreed on every one.
//
// The tester carries a bare `require('babel-eslint')` for its side effect, which the extractor
// stubs. Nothing in the corpus depends on that parser.

// forbidElementsOptions decodes a raw option body the way the config layer does.
//
// Fixtures route through the exported decoder rather than building the struct, which is what puts
// the string-or-object union under test. An empty body is the nil-options case: upstream reads
// `configuration.forbid || []`, so the answer is an empty list and the rule declines everything.
func forbidElementsOptions(test *testing.T, raw string) any {
	test.Helper()
	decoded, decodeForbidElementsOptionsError := DecodeForbidElementsOptions([]byte(raw))
	if decodeForbidElementsOptionsError != nil {
		test.Fatalf("decoding %q: %v", raw, decodeForbidElementsOptionsError)
	}
	return decoded
}

// TestForbidElementsFires runs the sixteen failing cases from upstream.
func TestForbidElementsFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		rawOptions string
		wantIds    []string
	}{
		{"invalid 0 a forbidden intrinsic", `<button />`, `{"forbid":["button"]}`, []string{"forbiddenElement"}},
		{"invalid 1 two forbidden elements in one array", `[<Modal />, <button />]`, `{"forbid":["button","Modal"]}`, []string{"forbiddenElement", "forbiddenElement"}},
		{"invalid 2 a dotted lowercase name", `<dotted.component />`, `{"forbid":["dotted.component"]}`, []string{"forbiddenElement"}},
		{"invalid 3 a dotted capitalised name with a note", `<dotted.Component />`, `{"forbid":[{"element":"dotted.Component","message":"that ain't cool"}]}`, []string{"forbiddenElement_message"}},
		{"invalid 4 an intrinsic with a note", `<button />`, `{"forbid":[{"element":"button","message":"use <Button> instead"}]}`, []string{"forbiddenElement_message"}},
		{"invalid 5 nested elements, both objects", `<button><input /></button>`, `{"forbid":[{"element":"button"},{"element":"input"}]}`, []string{"forbiddenElement", "forbiddenElement"}},
		{"invalid 6 nested elements, object then string", `<button><input /></button>`, `{"forbid":[{"element":"button"},"input"]}`, []string{"forbiddenElement", "forbiddenElement"}},
		{"invalid 7 nested elements, string then object", `<button><input /></button>`, `{"forbid":["input",{"element":"button"}]}`, []string{"forbiddenElement", "forbiddenElement"}},
		// The corpus's one statement that later entries win. The finding carries the second note,
		// which the message-text assertion below pins, since both entries share a message id.
		{"invalid 8 the same name twice keeps the last note", `<button />`, `{"forbid":[{"element":"button","message":"use <Button> instead"},{"element":"button","message":"use <Button2> instead"}]}`, []string{"forbiddenElement_message"}},
		{"invalid 9 a createElement literal with children", `React.createElement("button", {}, child)`, `{"forbid":["button"]}`, []string{"forbiddenElement"}},
		{"invalid 10 an identifier and a literal in one array", `[React.createElement(Modal), React.createElement("button")]`, `{"forbid":["button","Modal"]}`, []string{"forbiddenElement", "forbiddenElement"}},
		{"invalid 11 a createElement member with a note", `React.createElement(dotted.Component)`, `{"forbid":[{"element":"dotted.Component","message":"that ain't cool"}]}`, []string{"forbiddenElement_message"}},
		{"invalid 12 a createElement member, lowercase", `React.createElement(dotted.component)`, `{"forbid":["dotted.component"]}`, []string{"forbiddenElement"}},
		{"invalid 13 a createElement identifier starting with an underscore", `React.createElement(_comp)`, `{"forbid":["_comp"]}`, []string{"forbiddenElement"}},
		{"invalid 14 a createElement literal with a note", `React.createElement("button")`, `{"forbid":[{"element":"button","message":"use <Button> instead"}]}`, []string{"forbiddenElement_message"}},
		{"invalid 15 nested createElement calls", `React.createElement("button", {}, React.createElement("input"))`, `{"forbid":[{"element":"button"},{"element":"input"}]}`, []string{"forbiddenElement", "forbiddenElement"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test,
				ForbidElements,
				forbidElementsFile,
				testCase.sourceText,
				forbidElementsOptions(test, testCase.rawOptions),
			)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestForbidElementsStaysSilent runs the fourteen passing cases from upstream.
func TestForbidElementsStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		rawOptions string
	}{
		{"valid 0 no forbid list at all", `<button />`, `{}`},
		{"valid 1 an empty forbid list", `<button />`, `{"forbid":[]}`},
		{"valid 2 a capitalised component is a different name", `<Button />`, `{"forbid":["button"]}`},
		{"valid 3 the same, with the object spelling", `<Button />`, `{"forbid":[{"element":"button"}]}`},
		// A lowercase identifier fails upstream's `/^[A-Z_]/`, so nothing is looked up at all.
		{"valid 4 a lowercase createElement identifier", `React.createElement(button)`, `{"forbid":["button"]}`},
		// A bare call with no react import does not resolve to createElement.
		{"valid 5 a bare createElement call", `createElement("button")`, `{"forbid":["button"]}`},
		{"valid 6 a namespace that is not React", `NotReact.createElement("button")`, `{"forbid":["button"]}`},
		// The literal regex is `/^[a-z][^.]*$/`, so an underscore, a capital and a dot each fail it.
		{"valid 7 a literal starting with an underscore", `React.createElement("_thing")`, `{"forbid":["_thing"]}`},
		{"valid 8 a capitalised literal", `React.createElement("Modal")`, `{"forbid":["Modal"]}`},
		{"valid 9 a literal holding a dot", `React.createElement("dotted.component")`, `{"forbid":["dotted.component"]}`},
		{"valid 10 a function expression argument", `React.createElement(function() {})`, `{"forbid":["button"]}`},
		{"valid 11 an object argument", `React.createElement({})`, `{"forbid":["button"]}`},
		{"valid 12 a numeric literal argument", `React.createElement(1)`, `{"forbid":["button"]}`},
		{"valid 13 no arguments at all", `React.createElement()`, ``},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test,
				ForbidElements,
				forbidElementsFile,
				testCase.sourceText,
				forbidElementsOptions(test, testCase.rawOptions),
			)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// TestForbidElementsHasNoFileSuffixGate pins that the rule reads a plain `.ts` file.
//
// Three siblings in this package gate on `.tsx`/`.jsx`, inherited from oxc. This rule is ported
// from the authority, which has no such gate, and the createElement arm needs no JSX at all.
//
// `.jsx` and `.js` are not covered because the typed program's tsconfig includes only TypeScript
// extensions, which is a fact about the harness rather than about the rule. `.ts` is the extension
// that separates gated from ungated behaviour and it is the one the tree is mostly made of.
func TestForbidElementsHasNoFileSuffixGate(test *testing.T) {
	test.Parallel()

	for _, suffix := range []string{".tsx", ".ts"} {
		test.Run(suffix, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test,
				ForbidElements,
				"/repository/source/SuffixProbe"+suffix,
				`React.createElement("button");`,
				forbidElementsOptions(test, `{"forbid":["button"]}`),
			)
			rule_testing.ExpectFindings(test, result, "forbiddenElement")
		})
	}
}

// TestForbidElementsAnchorsOnTheName asserts where each finding points.
//
// The corpus asserts message ids only, so nothing in it can see a rule reporting the right judgment
// in the wrong place. Upstream reports on the name node for JSX and on the argument node for a
// createElement call, so neither finding covers the whole element or the whole call. Both were read
// from upstream's reported columns rather than from its source.
func TestForbidElementsAnchorsOnTheName(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		rawOptions string
		wantText   string
	}{
		{"a jsx name, not the tag", `<button />`, `{"forbid":["button"]}`, `button`},
		{"a dotted jsx name in full", `<dotted.component />`, `{"forbid":["dotted.component"]}`, `dotted.component`},
		{"a createElement literal with its quotes", `React.createElement("button")`, `{"forbid":["button"]}`, `"button"`},
		{"a createElement identifier", `React.createElement(Modal)`, `{"forbid":["Modal"]}`, `Modal`},
		{"a createElement member in full", `React.createElement(dotted.component)`, `{"forbid":["dotted.component"]}`, `dotted.component`},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			// RunTyped writes strings.TrimSpace(source)+"\n" to disk, so the expectation is
			// transformed the same way rather than sliced from the untransformed literal.
			onDisk := strings.TrimSpace(testCase.sourceText) + "\n"
			result := rule_testing.RunTypedWithOptions(
				test, ForbidElements, forbidElementsFile, testCase.sourceText,
				forbidElementsOptions(test, testCase.rawOptions),
			)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("want 1 finding, got %d", len(result.Diagnostics))
			}
			got := onDisk[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
			if got != testCase.wantText {
				test.Errorf("finding spans %q, want %q", got, testCase.wantText)
			}
		})
	}
}

// TestForbidElementsLaterEntriesWin pins the lookup's assignment order.
//
// Upstream builds a map keyed by element name and assigns in list order, so a name written twice
// keeps the last entry. The corpus states this once, for two objects sharing a message id, which
// means an id assertion cannot see which of the two won. The rendered text can, and the mixed
// spellings were measured against the installed build because the corpus writes neither.
func TestForbidElementsLaterEntriesWin(test *testing.T) {
	test.Parallel()

	test.Run("the last of two notes is the one reported", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTypedWithOptions(
			test, ForbidElements, forbidElementsFile, `<button />`,
			forbidElementsOptions(test, `{"forbid":[{"element":"button","message":"use <Button> instead"},{"element":"button","message":"use <Button2> instead"}]}`),
		)
		rule_testing.ExpectFindings(test, result, "forbiddenElement_message")
		got := result.Diagnostics[0].Message.Description
		if !strings.HasSuffix(got, ": use <Button2> instead") {
			test.Errorf("reported the wrong note: %q", got)
		}
	})

	// A string entry after an object entry erases the note, which is the direction a port storing
	// the first match would get backwards.
	test.Run("a bare string after an object drops the note", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTypedWithOptions(
			test, ForbidElements, forbidElementsFile, `<button />`,
			forbidElementsOptions(test, `{"forbid":[{"element":"button","message":"m"},"button"]}`),
		)
		rule_testing.ExpectFindings(test, result, "forbiddenElement")
	})

	test.Run("an object after a bare string adds the note", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTypedWithOptions(
			test, ForbidElements, forbidElementsFile, `<button />`,
			forbidElementsOptions(test, `{"forbid":["button",{"element":"button","message":"m"}]}`),
		)
		rule_testing.ExpectFindings(test, result, "forbiddenElement_message")
	})
}

// TestForbidElementsEmptyMessageIsNotAMessage pins upstream's truthiness test.
//
// `message ? messages.forbiddenElement_message : messages.forbiddenElement` takes the else branch on
// an empty string, so an entry carrying `message: ""` reports the plain id. Measured against the
// installed build. A port testing for the key's presence rather than its truthiness would report a
// message id with an empty tail, and no corpus case writes an empty message.
func TestForbidElementsEmptyMessageIsNotAMessage(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunTypedWithOptions(
		test, ForbidElements, forbidElementsFile, `<button />`,
		forbidElementsOptions(test, `{"forbid":[{"element":"button","message":""}]}`),
	)
	rule_testing.ExpectFindings(test, result, "forbiddenElement")
}

// TestForbidElementsArgumentShapes covers the two regexes and the member arm.
//
// This is where nearly all of upstream's judgment sits and the corpus states only part of it. Every
// case here was measured against the installed build before it was written.
func TestForbidElementsArgumentShapes(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		rawOptions string
		wantIds    []string
	}{
		// The identifier regex is `/^[A-Z_]/`, tested at both accepted characters.
		{"an uppercase identifier reports", `React.createElement(Modal)`, `{"forbid":["Modal"]}`, []string{"forbiddenElement"}},
		{"an underscore identifier reports", `React.createElement(_x)`, `{"forbid":["_x"]}`, []string{"forbiddenElement"}},
		{"a lowercase identifier declines", `React.createElement(button)`, `{"forbid":["button"]}`, nil},
		// The literal regex is `/^[a-z][^.]*$/`, anchored at both ends.
		{"a dash inside a literal is fine", `React.createElement("my-el")`, `{"forbid":["my-el"]}`, []string{"forbiddenElement"}},
		{"a digit first fails the literal regex", `React.createElement("1x")`, `{"forbid":["1x"]}`, nil},
		{"a template literal is not a Literal upstream", "React.createElement(`button`)", `{"forbid":["button"]}`, nil},
		// The one value in upstream's Literal set whose stringification matches the lowercase
		// regex. Nothing in the corpus writes it, and it is reproduced rather than corrected: the
		// alternative is a port that quietly disagrees with the running rule.
		{"a boolean keyword reaches the lookup as its text", `React.createElement(true)`, `{"forbid":["true"]}`, []string{"forbiddenElement"}},
		{"a false keyword likewise", `React.createElement(false)`, `{"forbid":["false"]}`, []string{"forbiddenElement"}},
		// The member arm reads source text, so the spelling survives into the lookup key. A port
		// rebuilding the name from the tree would answer "a.b" for all three of these.
		{"a deep member reports its whole path", `React.createElement(a.b.c)`, `{"forbid":["a.b.c"]}`, []string{"forbiddenElement"}},
		{"a computed member keeps its brackets and quotes", `React.createElement(a["b"])`, `{"forbid":["a[\"b\"]"]}`, []string{"forbiddenElement"}},
		{"a spaced member keeps its spaces", `React.createElement(a . b)`, `{"forbid":["a . b"]}`, []string{"forbiddenElement"}},
		{"a spaced member does not match the tight spelling", `React.createElement(a . b)`, `{"forbid":["a.b"]}`, nil},
		// The JSX arm reads source text too, which is why a namespaced name arrives whole.
		{"a namespaced jsx name reports with its colon", `<svg:rect />`, `{"forbid":["svg:rect"]}`, []string{"forbiddenElement"}},
		{"a deep jsx member name reports whole", `<a.b.c />`, `{"forbid":["a.b.c"]}`, []string{"forbiddenElement"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test, ForbidElements, forbidElementsFile, testCase.sourceText,
				forbidElementsOptions(test, testCase.rawOptions),
			)
			if len(testCase.wantIds) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestForbidElementsCreateElementResolution covers which callees count.
//
// The corpus pins two declines directly, and the accepting shapes below were measured. This is the
// same resolution three sibling rules already share, so the value here is pinning that this rule
// uses it rather than the shelf's wider `react.IsCreateElementCall`, which accepts a bare call with
// no import and would report on upstream's own clean case.
func TestForbidElementsCreateElementResolution(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{"a bare call with no import declines", `createElement("button");`, nil},
		{"a foreign namespace declines", `NotReact.createElement("button");`, nil},
		{"a computed member declines", `React['createElement']("button");`, nil},
		{"a destructured react import reports", "import {createElement} from 'react';\ncreateElement(\"button\");", []string{"forbiddenElement"}},
		{"a destructured preact import declines", "import {createElement} from 'preact';\ncreateElement(\"button\");", nil},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTypedWithOptions(
				test, ForbidElements, forbidElementsFile, testCase.sourceText,
				forbidElementsOptions(test, `{"forbid":["button"]}`),
			)
			if len(testCase.wantIds) == 0 {
				rule_testing.ExpectClean(test, result)
				return
			}
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)
		})
	}
}

// TestForbidElementsRequiresTheTypedHarness pins the checker declaration.
//
// The bare-identifier branch of the createElement resolution asks the checker, so under the plain
// harness that arm guards and goes silent while the JSX arm keeps reporting. A later revert of
// `NeedsTypeChecker` fails here rather than producing a vacuous green, and the JSX half is what
// makes this a discrimination rather than a blanket silence.
func TestForbidElementsRequiresTheTypedHarness(test *testing.T) {
	test.Parallel()

	source := "import {createElement} from 'react';\ncreateElement(\"button\");"
	options := forbidElementsOptions(test, `{"forbid":["button"]}`)

	untyped := rule_testing.RunWithOptions(test, ForbidElements, forbidElementsFile, source, options)
	rule_testing.ExpectClean(test, untyped)

	typed := rule_testing.RunTypedWithOptions(test, ForbidElements, forbidElementsFile, source, options)
	rule_testing.ExpectFindings(test, typed, "forbiddenElement")

	jsxUntyped := rule_testing.RunWithOptions(test, ForbidElements, forbidElementsFile, `<button />`, options)
	rule_testing.ExpectFindings(test, jsxUntyped, "forbiddenElement")
}

// TestDecodeForbidElementsOptions covers the union the schema declares and Go has no shape for.
//
// The items are `anyOf: [string, object]`, so the decoder tries a string first and falls back to
// the object. Both arms and the nil path are asserted here, because a fixture handing the rule a
// pre-built struct would leave the whole decoder untested, and the union is the one line most
// likely to have no upstream counterpart.
func TestDecodeForbidElementsOptions(test *testing.T) {
	test.Parallel()

	test.Run("an empty body yields an empty list rather than an error", func(test *testing.T) {
		test.Parallel()
		decoded, decodeForbidElementsOptionsError := DecodeForbidElementsOptions(nil)
		if decodeForbidElementsOptionsError != nil {
			test.Fatalf("unexpected error: %v", decodeForbidElementsOptionsError)
		}
		options, ok := decoded.(ForbidElementsOptions)
		if !ok {
			test.Fatalf("decoded to %T", decoded)
		}
		if len(options.Forbid) != 0 {
			test.Errorf("want an empty list, got %d entries", len(options.Forbid))
		}
	})

	test.Run("both arms of the union decode", func(test *testing.T) {
		test.Parallel()
		decoded, decodeForbidElementsOptionsError := DecodeForbidElementsOptions([]byte(`{"forbid":["a",{"element":"b","message":"m"}]}`))
		if decodeForbidElementsOptionsError != nil {
			test.Fatalf("unexpected error: %v", decodeForbidElementsOptionsError)
		}
		options, ok := decoded.(ForbidElementsOptions)
		if !ok {
			test.Fatalf("decoded to %T, want ForbidElementsOptions", decoded)
		}
		if len(options.Forbid) != 2 {
			test.Fatalf("want 2 entries, got %d", len(options.Forbid))
		}
		if options.Forbid[0].Element != "a" || options.Forbid[0].Message != "" {
			test.Errorf("string arm decoded to %+v", options.Forbid[0])
		}
		if options.Forbid[1].Element != "b" || options.Forbid[1].Message != "m" {
			test.Errorf("object arm decoded to %+v", options.Forbid[1])
		}
	})

	test.Run("order is preserved, which is what makes the last entry win", func(test *testing.T) {
		test.Parallel()
		decoded, decodeForbidElementsOptionsError := DecodeForbidElementsOptions([]byte(`{"forbid":["x","y","z"]}`))
		if decodeForbidElementsOptionsError != nil {
			test.Fatalf("unexpected error: %v", decodeForbidElementsOptionsError)
		}
		options, ok := decoded.(ForbidElementsOptions)
		if !ok {
			test.Fatalf("decoded to %T, want ForbidElementsOptions", decoded)
		}
		got := []string{options.Forbid[0].Element, options.Forbid[1].Element, options.Forbid[2].Element}
		if got[0] != "x" || got[1] != "y" || got[2] != "z" {
			test.Errorf("order is %v", got)
		}
	})

	test.Run("a malformed body errors rather than silently emptying the list", func(test *testing.T) {
		test.Parallel()
		if _, decodeForbidElementsOptionsError := DecodeForbidElementsOptions([]byte(`{"forbid":`)); decodeForbidElementsOptionsError == nil {
			test.Error("want an error for a truncated body")
		}
		if _, decodeForbidElementsOptionsError := DecodeForbidElementsOptions([]byte(`{"forbid":[1]}`)); decodeForbidElementsOptionsError == nil {
			test.Error("want an error for an entry that is neither a string nor an object")
		}
	})
}
