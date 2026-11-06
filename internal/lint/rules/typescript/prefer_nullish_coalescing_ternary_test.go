package typescript

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// preferNullishCoalescingReplayed is one finding as the installed typescript-eslint 8.67.0 reported
// it: the id, where it starts, the exact text it covers, and the whole file after its one suggestion.
//
// Line and column are 1-based in the file as the typed harness writes it, which is the source
// trimmed of surrounding whitespace, so they read directly against the fixture.
type preferNullishCoalescingReplayed struct {
	id        string
	line      int
	column    int
	reported  string
	suggested string
}

type preferNullishCoalescingReplayedCase struct {
	name        string
	source      string
	optionsJson string
	findings    []preferNullishCoalescingReplayed
}

// preferNullishCoalescingBeyondTheCorpusCases report where upstream's corpus never looks, each
// replayed through the installed build the same way.
//
// A mutation sweep over the new paths left two checks no corpus case could fail. A join mixing `===`
// with `==` reads as loose, and a loose comparison rules out null and undefined both, so a test that
// names only null that way is fixable whatever the type; read as strict, `string | null | undefined`
// would make it unfixable. These rows, both arms of the join and the if path, are what kill it.
var preferNullishCoalescingBeyondTheCorpusCases = []preferNullishCoalescingReplayedCase{
	{
		name:   "replayed: === and == joined by || read loose, so null alone is enough",
		source: "declare let x: string | null | undefined;\ndeclare const y: string;\nconst r = x === null || x == null ? y : x;\n",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      3,
				column:    11,
				reported:  `x === null || x == null ? y : x`,
				suggested: "declare let x: string | null | undefined;\ndeclare const y: string;\nconst r = x ?? y;\n",
			},
		},
	},
	{
		name:   "replayed: !== and != joined by && read loose",
		source: "declare let x: string | null | undefined;\ndeclare const y: string;\nconst r = x !== null && x != null ? x : y;\n",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverTernary",
				line:      3,
				column:    11,
				reported:  `x !== null && x != null ? x : y`,
				suggested: "declare let x: string | null | undefined;\ndeclare const y: string;\nconst r = x ?? y;\n",
			},
		},
	},
	{
		name:   "replayed: the if path reads the same join loose",
		source: "declare let x: string | null | undefined;\ndeclare const y: string;\nif (x === null || x == null) { x = y; }\n",
		findings: []preferNullishCoalescingReplayed{
			{
				id:        "preferNullishOverAssignment",
				line:      3,
				column:    1,
				reported:  `if (x === null || x == null) { x = y; }`,
				suggested: "declare let x: string | null | undefined;\ndeclare const y: string;\nx ??= y;\n",
			},
		},
	},
}

// TestPreferNullishCoalescingTernaryAndIfStaySilentBeyondTheCorpus is the other survivor's control.
//
// A test that compares its subject only to itself rules out neither null nor undefined, so upstream
// declines it; a port answering "fixable" there is silent on every corpus case and wrong here. The
// strict `=== null || === null` row is the loose join's own control: the same shape read strict, on
// a type that also holds undefined, is not fixable. All four replayed silent on the installed build.
func TestPreferNullishCoalescingTernaryAndIfStaySilentBeyondTheCorpus(test *testing.T) {
	test.Parallel()
	for _, source := range []string{
		"declare let x: string | null | undefined;\ndeclare const y: string;\nconst r = x === null || x === null ? y : x;\n",
		"declare let x: string | null;\ndeclare const y: string;\nconst r = x !== x ? x : y;\n",
		"declare let x: string | null;\ndeclare const y: string;\nconst r = x === x || x === x ? y : x;\n",
		"declare let x: string | null;\ndeclare const y: string;\nif (x === x) x = y;\n",
	} {
		test.Run(source, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(
				test,
				runPreferNullishCoalescing(test, preferNullishCoalescingCase{source: source}),
			)
		})
	}
}

// TestPreferNullishCoalescingTernaryAndIfMessages pins the two new renderings whole, since the
// replay table asserts ids and the message text is ours past upstream's first sentence.
func TestPreferNullishCoalescingTernaryAndIfMessages(test *testing.T) {
	test.Parallel()
	ternary := runPreferNullishCoalescing(test, preferNullishCoalescingCase{
		source: "declare const a: string | null;\nconst x = a !== null ? a : 'b';\n",
	})
	assignment := runPreferNullishCoalescing(test, preferNullishCoalescingCase{
		source: "declare let a: string | null;\nif (!a) a = 'b';\n",
	})
	for _, check := range []struct {
		name    string
		result  rule_testing.Result
		opening string
	}{
		{"ternary", ternary,
			"Prefer using nullish coalescing operator (`??`) instead of a ternary expression, as it is simpler to read."},
		{"assignment", assignment,
			"Prefer using nullish coalescing operator (`??=`) instead of an assignment expression, as it is simpler to read."},
	} {
		if len(check.result.Diagnostics) != 1 ||
			!strings.HasPrefix(check.result.Diagnostics[0].Message.Description, check.opening) {
			test.Errorf("%s: expected one finding opening with upstream's sentence %q, got %v",
				check.name, check.opening, check.result.Diagnostics)
		}
	}
}

// preferNullishCoalescingTernaryAndIfCases are upstream's invalid cases that report from the ternary
// or if-statement path, generated from the replay described on TestPreferNullishCoalescingTernaryAndIfFire.
// Each path's table lives in files of its own, the ternary's in two split at invalid[95], since one would
// pass the long-file limit by itself, and this joins them in upstream's order.
var preferNullishCoalescingTernaryAndIfCases = append(append(append([]preferNullishCoalescingReplayedCase(nil),
	preferNullishCoalescingTernaryComparisonCases...), preferNullishCoalescingTernaryDeclaredCases...),
	preferNullishCoalescingIfCases...)

// TestPreferNullishCoalescingTernaryAndIfFire runs every upstream invalid case that reports from the
// ternary or the if-statement path, 221 of them, two of which also report a `||`.
//
// # Every expectation was replayed through the installed build, not copied from upstream's file
//
// Upstream's corpus was extracted by running its test file against a stub RuleTester, and each case
// was then linted by ESLint 10 with typescript-eslint 8.67.0 on a strict program. The replay agreed
// with upstream's own expected ids, spans and suggestion outputs on all 344 strict cases, so the rows
// it runs are both: what upstream asserts and what the installed rule does.
//
// They assert more than the imported `||` corpus does. A ternary's suggestion rewrites the whole
// expression, choosing the subject's spelling (the first mention, with its optional links), keeping
// one pair of its parentheses, and parenthesising the fallback by precedence; an if's suggestion
// carries comments out of the block. An id and a count see none of that, so each row pins the
// spanned text and the whole file after the suggestion.
func TestPreferNullishCoalescingTernaryAndIfFire(test *testing.T) {
	test.Parallel()
	cases := append(append([]preferNullishCoalescingReplayedCase(nil),
		preferNullishCoalescingTernaryAndIfCases...), preferNullishCoalescingBeyondTheCorpusCases...)
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runPreferNullishCoalescing(test, preferNullishCoalescingCase{
				source: testCase.source, optionsJson: testCase.optionsJson,
			})
			wantIds := make([]string, 0, len(testCase.findings))
			for _, expected := range testCase.findings {
				wantIds = append(wantIds, expected.id)
			}
			rule_testing.ExpectFindings(test, result, wantIds...)

			text := asTheTypedPreferNullishHarnessWroteIt(testCase.source)
			for index, expected := range testCase.findings {
				diagnostic := result.Diagnostics[index]

				start := preferNullishCoalescingOffsetOf(test, testCase.source, expected.line, expected.column)
				if diagnostic.Range.Pos() != start || diagnostic.Range.End() != start+len(expected.reported) {
					test.Errorf("finding %d: expected the span %q at line %d column %d, got %q",
						index, expected.reported, expected.line, expected.column,
						text[diagnostic.Range.Pos():diagnostic.Range.End()])
				}

				if len(diagnostic.Suggestions) != 1 {
					test.Fatalf("finding %d: expected one suggestion, got %d", index, len(diagnostic.Suggestions))
				}
				// Applied from the back, so an earlier edit never shifts a later one's offsets.
				fixes := append([]rule.Fix(nil), diagnostic.Suggestions[0].Fixes...)
				sort.Slice(fixes, func(left, right int) bool {
					return fixes[left].Range.Pos() > fixes[right].Range.Pos()
				})
				applied := text
				for _, fix := range fixes {
					applied = applied[:fix.Range.Pos()] + fix.Text + applied[fix.Range.End():]
				}
				if applied != expected.suggested {
					test.Errorf("finding %d: the suggestion writes\n%s\nupstream's writes\n%s",
						index, applied, expected.suggested)
				}
			}
		})
	}
}

// preferNullishCoalescingCompiledRows is the table both engines run: each source holds one `if` the
// check reports with the compiler off, and `compiled` says whether React Compiler compiles the function
// holding it.
var preferNullishCoalescingCompiledRows = []struct {
	name       string
	sourceText string
	compiled   bool
}{
	{"a component", `export function Badge(properties: { label: string | null }) {
    let label = properties.label;
    if(label === null) label = 'none';
    return <span>{label}</span>;
}
`, true},
	{"an async callback inside a component, as SecretRow", `import React from 'react';
declare function reveal(): Promise<string | null>;
export function SecretRow(properties: { revealed: string | null }) {
    const onCopy = React.useCallback(async function() {
        let value: string | null = properties.revealed;
        if(value === null) {
            value = await reveal();
        }
        return value;
    }, [properties.revealed]);
    return <button onClick={onCopy}>copy</button>;
}
`, true},
	{"an effect callback inside a provider, as WebSocketViaSharedWorkerProviderInternal", `import React from 'react';
declare function createMonitor(): object;
export function WebSocketProviderInternal(properties: { children: React.ReactNode }) {
    const monitorReference: { current: object | null } = React.useRef(null);
    React.useEffect(function() {
        if(monitorReference.current === null) {
            monitorReference.current = createMonitor();
        }
    }, []);
    return <>{properties.children}</>;
}
`, true},
	{"a hook body", `import React from 'react';
export function useStart(initial: number | null) {
    const [count] = React.useState(0);
    let start = initial;
    if(start === null) start = count;
    return start;
}
`, true},
	{"a hook named with a digit after use", `import { useState } from 'react';
export function use2Things(initial: number | null) {
    const [count] = useState(0);
    let start = initial;
    if(start === null) start = count;
    return start;
}
`, true},
	{"a component inside a component-named function with no evidence of its own", `export function Outer() {
    const Inner = (properties: { label: string | null }) => {
        let label = properties.label;
        if(label === null) label = 'none';
        return <span>{label}</span>;
    };
    return Inner;
}
`, true},
	{
		"a component that takes a ref second",
		`export function Field(properties: { label: string | null }, forwardedRef: unknown) {
    let label = properties.label;
    if(label === null) label = 'none';
    return <span ref={forwardedRef}>{label}</span>;
}
`,
		true,
	},
	{"a helper outside any component", `export function normalize(stored: string | null) {
    let label = stored;
    if(label === null) label = 'none';
    return label;
}
`, false},
	{"a hook-named function that calls no hook and writes no JSX", `export function useLabel(stored: string | null) {
    let label = stored;
    if(label === null) label = 'none';
    return label;
}
`, false},
	{
		"a component-named function with no JSX and no hooks",
		`export function Defaults(properties: { label: string | null }) {
    let label = properties.label;
    if(label === null) label = 'none';
    return label;
}
`,
		false,
	},
	{"a component inside a bare block", `{
    function Badge(properties: { label: string | null }) {
        let label = properties.label;
        if(label === null) label = 'none';
        return <span>{label}</span>;
    }
    console.info(Badge);
}
`, false},
	{"a component inside a function whose name claims nothing", `export function plainOuter() {
    const Inner = (properties: { label: string | null }) => {
        let label = properties.label;
        if(label === null) label = 'none';
        return <span>{label}</span>;
    };
    return Inner;
}
`, false},
	{"a component wrapped in a call, which names nothing", `declare function memo<Type>(component: Type): Type;
export const Badge = memo(function(properties: { label: string | null }) {
    let label = properties.label;
    if(label === null) label = 'none';
    return <span>{label}</span>;
});
`, false},
	{"a component name with a non-ASCII capital", `export function Émile(properties: { label: string | null }) {
    let label = properties.label;
    if(label === null) label = 'none';
    return <span>{label}</span>;
}
`, false},
	{"a component whose props are annotated as a primitive", `export function Badge(text: string) {
    let label: string | null = text.length > 0 ? text : null;
    if(label === null) label = 'none';
    return <span>{label}</span>;
}
`, false},
	{
		"a component whose second parameter is not a ref",
		`export function Badge(properties: { label: string | null }, other: unknown) {
    let label = properties.label;
    if(label === null) label = 'none';
    return <span>{label}{String(other)}</span>;
}
`,
		false,
	},
	{"a component whose only JSX is inside a closure", `export function Badge(properties: { label: string | null }) {
    let label = properties.label;
    if(label === null) label = 'none';
    return () => <span>{label}</span>;
}
`, false},
}

// TestPreferNullishCoalescingIfIsSilentInsideReactCompiledFunctions covers the `reactCompiler` option,
// which is ours rather than upstream's, on the if-statement check it gates.
//
// The rows are shared verbatim with Nexus's NexusTypeScriptEsLintPlugin.test.ts, whose wrapper draws the
// same line in ESLint, so one table proves the ruling in both engines (#cn8sthd). Three are the sites
// the waves rewrote to `??=`, each of which lost its compilation to the rewrite, in their long form:
// SecretRow, WebSocketViaSharedWorkerProviderInternal and ChatReasoningAndTools' component body. The
// rest pin where the compiled region ends, from the measurements recorded in the react shelf's
// compiled.go: a bare block, an outer function whose name claims nothing, a call wrapper, a non-ASCII
// capital, primitive props, a second parameter that is not a ref, and JSX only inside a closure.
//
// Every row runs with the compiler on and off, so a silent row is shown to be the gate rather than a
// shape the check never reported. The harness has no React types, so each ref is annotated: read as
// `any`, its `current` is not nullable and the check would never fire.
func TestPreferNullishCoalescingIfIsSilentInsideReactCompiledFunctions(test *testing.T) {
	test.Parallel()

	decode := func(raw string) PreferNullishCoalescingOptions {
		decoded, decodePreferNullishCoalescingOptionsError := DecodePreferNullishCoalescingOptions(json.RawMessage(raw))
		if decodePreferNullishCoalescingOptionsError != nil {
			test.Fatalf("decoding %s: %v", raw, decodePreferNullishCoalescingOptionsError)
		}
		return decoded
	}
	compilerOn := decode(`{}`)
	compilerOff := decode(`{"reactCompiler": false}`)

	for _, testCase := range preferNullishCoalescingCompiledRows {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			off := rule_testing.RunTypedWithOptions(
				test,
				PreferNullishCoalescing,
				"Compiled.tsx",
				testCase.sourceText,
				compilerOff,
			)
			rule_testing.ExpectFindings(test, off, "preferNullishOverAssignment")
			on := rule_testing.RunTypedWithOptions(
				test,
				PreferNullishCoalescing,
				"Compiled.tsx",
				testCase.sourceText,
				compilerOn,
			)
			if testCase.compiled {
				rule_testing.ExpectClean(test, on)
				return
			}
			rule_testing.ExpectFindings(test, on, "preferNullishOverAssignment")
		})
	}

	// The ternary's repair is `??`, an expression the compiler lowers, so it still reports in a
	// component with the compiler on.
	ternary := rule_testing.RunTypedWithOptions(test, PreferNullishCoalescing, "Compiled.tsx", `
export function Badge(properties: { label: string | null }) {
    const label = properties.label !== null ? properties.label : 'none';
    return <span>{label}</span>;
}`, compilerOn)
	rule_testing.ExpectFindings(test, ternary, "preferNullishOverTernary")
}
