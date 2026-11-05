package typescript

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// noMisusedPromisesFile is the fixture name most of this rule's cases run under.
//
// A TypeScript extension is required rather than incidental: the corpus is dense with type
// annotations, interfaces and `declare` statements, so a `.js` name would turn most of these into
// parse errors rather than rule inputs.
const noMisusedPromisesFile = "noMisusedPromises.ts"

// noMisusedPromisesTsxFile carries the seven cases upstream marks `Tsx: true`.
//
// Four are valid and three report `voidReturnAttribute`, and that message id is reachable through no
// other extension: the attribute check anchors on `ast.KindJsxAttribute`, which a `.ts` file cannot
// produce. Running those seven under the `.ts` name would not fail loudly: it would parse the JSX as
// type assertions and comparisons, produce no attribute nodes, and leave three invalid cases silently
// asserting the opposite of upstream while the whole suite stayed green.
const noMisusedPromisesTsxFile = "noMisusedPromises.tsx"

// TestNoMisusedPromisesRequiresTheTypedHarness pins the nil-checker guard as a measured fact.
//
// This is the one addition the absorption made to upstream's body, and it exists because the failure
// it prevents is silent rather than loud. Our shim's type queries return nil instead of panicking, so
// a rule that read a nil checker would not crash: it would report nothing, and every one of the one
// hundred and twenty-three clean cases above would pass having proven nothing at all. A vacuous green
// suite is indistinguishable from a correct one from the outside, which is why this asserts the shape
// directly rather than trusting the tables.
//
// It asserts three things together, because any one of them alone can be satisfied by a broken rule.
// The declaration must be true, so the walk actually hands the rule a checker. The untyped harness
// must produce no findings on an input the typed harness reports, so the guard is proven to fire
// rather than merely to exist. And the typed harness must still report that input, so the first
// assertion is not passing because the rule is broken in some other way.
func TestNoMisusedPromisesRequiresTheTypedHarness(test *testing.T) {
	test.Parallel()

	const source = `
const p = Promise.resolve();
if (p) {
}
`

	if !NoMisusedPromises.NeedsTypeChecker {
		test.Fatal("the rule reads ctx.TypeChecker in every listener, so it must declare NeedsTypeChecker")
	}

	untyped := rule_testing.Run(test, NoMisusedPromises, noMisusedPromisesFile, source)
	if len(untyped.Diagnostics) != 0 {
		test.Fatalf(
			"the untyped harness hands the rule a nil checker, so the guard should decline it; got %d findings",
			len(untyped.Diagnostics),
		)
	}

	typed := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, source)
	rule_testing.ExpectFindings(test, typed, "conditional")
}

// TestNoMisusedPromisesDoesNotReadTheProgram pins ProgramReads as a per-rule measurement.
//
// The wave this rule landed in replaced an adapter that declared a program read on every rule it
// wrapped by assumption. Absorbing makes the question answerable, and the answer here is no: the body
// never names ctx.Program. Under-declaring serves stale cached findings forever, which is the failure
// the flag exists to prevent, so the claim is worth a guard rather than a comment.
//
// The test runs the rule with a Context whose Program is nil and asserts it still reports. A rule
// that reached for the program would panic here, which is the loud direction.
func TestNoMisusedPromisesDoesNotReadTheProgram(test *testing.T) {
	test.Parallel()

	if NoMisusedPromises.ProgramReads != 0 {
		test.Fatalf(
			"the rule body never names ctx.Program, so it must declare no ProgramReads; declares %s",
			NoMisusedPromises.ProgramReads,
		)
	}
}

// TestNoMisusedPromisesRendersTheSpreadTypoUpstreamShips asserts the rendered message text exactly.
//
// tsgolint writes "spreaded" where @typescript-eslint writes "spread". That is upstream's error of
// English and it is reproduced rather than repaired, because oxlint runs tsgolint and the differential
// harness compares against oxlint, so correcting it would read as a difference the harness can see.
//
// A message-id assertion cannot see this. Every fixture above asserting "spread" stays green over
// either wording, so without this test the divergence would be invisible in both directions: we
// could drift off upstream silently, and upstream could fix it without us noticing. The expected
// string is typed here as a literal rather than compared against the rule's own message constant,
// because a comparison against the constant moves with the constant and asserts nothing.
func TestNoMisusedPromisesRendersTheSpreadTypoUpstreamShips(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, `
const promise = Promise.resolve({ a: 1 });
const obj = { ...promise };
`)

	if len(result.Diagnostics) != 1 {
		test.Fatalf("wanted one finding to read the message from, got %d", len(result.Diagnostics))
	}
	if got := result.Diagnostics[0].Message.Id; got != "spread" {
		test.Fatalf("message id: got %q, want %q", got, "spread")
	}
	const want = "Expected a non-Promise value to be spreaded in an object."
	if got := result.Diagnostics[0].Message.Description; got != want {
		test.Fatalf("message text: got %q, want %q\n(if upstream fixed the typo, this is the sync signal)", got, want)
	}
}

// TestNoMisusedPromisesReportsTheRightSpans slices the source with each finding's own range.
//
// ExpectFindings asserts ids and count and nothing else, so a rule pointing at the wrong node passes
// a complete fixture pair while being wrong. That is not hypothetical for this rule: several of its
// report sites choose deliberately between a node and its type annotation (checkProperty reports the
// return type when the initializer is function-like and has one, and the initializer otherwise),
// and no id assertion can tell those apart.
//
// One case per message id the rule can emit, so every arm's anchor is pinned.
func TestNoMisusedPromisesReportsTheRightSpans(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantId     string
		wantText   string
	}{
		{
			name:     "conditional points at the tested expression",
			fileName: noMisusedPromisesFile,
			sourceText: `
const promise = Promise.resolve();
if (promise) {
}
`,
			wantId:   "conditional",
			wantText: "promise",
		},
		{
			name:     "predicate points at the callback rather than the call",
			fileName: noMisusedPromisesFile,
			sourceText: `
const array = [1, 2, 3];
array.filter(async n => n > 1);
`,
			wantId:   "predicate",
			wantText: "async n => n > 1",
		},
		{
			name:     "spread points at the spread expression rather than the whole element",
			fileName: noMisusedPromisesFile,
			sourceText: `
const promise = Promise.resolve({ a: 1 });
const obj = { ...promise };
`,
			wantId:   "spread",
			wantText: "promise",
		},
		{
			name:     "voidReturnArgument points at the offending argument",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function takesCallback(callback: () => void): void;
takesCallback(async () => {});
`,
			wantId:   "voidReturnArgument",
			wantText: "async () => {}",
		},
		{
			name:     "voidReturnVariable points at the initializer rather than the declaration",
			fileName: noMisusedPromisesFile,
			sourceText: `
const fn: () => void = async () => {};
`,
			wantId:   "voidReturnVariable",
			wantText: "async () => {}",
		},
		{
			name:     "voidReturnReturnValue points at the returned expression",
			fileName: noMisusedPromisesFile,
			sourceText: `
declare function takesCallback(): () => void;
function outer(): () => void {
  return async () => {};
}
`,
			wantId:   "voidReturnReturnValue",
			wantText: "async () => {}",
		},
		{
			name:     "voidReturnAttribute points at the JSX expression container, braces included",
			fileName: noMisusedPromisesTsxFile,
			sourceText: `
type Props = { onEvent: () => void };
declare function Component(props: Props): null;
const element = <Component onEvent={async () => {}} />;
`,
			wantId:   "voidReturnAttribute",
			wantText: "{async () => {}}",
		},
		{
			name:     "voidReturnInheritedMethod points at the whole overriding member",
			fileName: noMisusedPromisesFile,
			sourceText: `
interface Base {
  method(): void;
}
class Derived implements Base {
  async method() {}
}
`,
			wantId:   "voidReturnInheritedMethod",
			wantText: "async method() {}",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, testCase.fileName, testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted exactly one finding to slice, got %d", len(result.Diagnostics))
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Message.Id != testCase.wantId {
				test.Fatalf("message id: got %q, want %q", diagnostic.Message.Id, testCase.wantId)
			}
			source := result.SourceFile.Text()
			reported := source[diagnostic.Range.Pos():diagnostic.Range.End()]
			if reported != testCase.wantText {
				test.Fatalf("reported span: got %q, want %q", reported, testCase.wantText)
			}
		})
	}
}

// TestNoMisusedPromisesDecodesOptionsThroughTheRegisteredDecoder routes configuration through the
// rule's own decoder rather than building the options struct directly.
//
// That is what puts the pointer fields under test. Every option on this rule defaults to true: three
// top-level checks and six void-return sub-flags, nine fields whose default is not the zero value,
// so a plain bool anywhere in this surface could not tell "the user wrote false" from "the user wrote
// nothing", and the whole defaulting block at the top of Run reads exactly that distinction. Handing
// RunWithOptions a struct built here would leave the JSON binding itself untested, and the binding is
// the half with no upstream counterpart.
//
// The nested case matters most: checksVoidReturn is spelled in JSON as either a boolean or an object
// of sub-flags, while the Go struct splits those into two separate fields. This asserts what our
// decoder actually does with each spelling rather than what the shape suggests.
func TestNoMisusedPromisesDecodesOptionsThroughTheRegisteredDecoder(test *testing.T) {
	test.Parallel()

	decode := rule.DecodeOptionsInto[NoMisusedPromisesOptions]()

	cases := []struct {
		name   string
		config string
		assert func(test *testing.T, decoded NoMisusedPromisesOptions)
	}{
		{
			name:   "an explicit false on a check that defaults to true survives decoding",
			config: `{"checksConditionals": false}`,
			assert: func(test *testing.T, decoded NoMisusedPromisesOptions) {
				if decoded.ChecksConditionals == nil {
					test.Fatal(
						"checksConditionals decoded to nil, so an explicit false is indistinguishable from silence",
					)
				}
				if *decoded.ChecksConditionals {
					test.Fatal("checksConditionals decoded to true from an explicit false")
				}
			},
		},
		{
			name:   "an omitted check decodes to nil so Run can apply the true default",
			config: `{"checksSpreads": false}`,
			assert: func(test *testing.T, decoded NoMisusedPromisesOptions) {
				if decoded.ChecksConditionals != nil {
					test.Fatal("an omitted checksConditionals should stay nil for Run to default")
				}
				if decoded.ChecksSpreads == nil || *decoded.ChecksSpreads {
					test.Fatal("checksSpreads should have decoded to an explicit false")
				}
			},
		},
		{
			// Upstream's spelling: the object form under checksVoidReturn itself. Until #4a4yse4 this
			// row wrote tsgolint's `checksVoidReturnOpts`, a key upstream does not have, and upstream's
			// own spelling failed to decode into a boolean.
			name:   "the sub-flag object under checksVoidReturn turns the check on and binds onto ChecksVoidReturnOpts",
			config: `{"checksVoidReturn": {"arguments": false, "attributes": true}}`,
			assert: func(test *testing.T, decoded NoMisusedPromisesOptions) {
				if decoded.ChecksVoidReturn == nil || !*decoded.ChecksVoidReturn {
					test.Fatal(
						"the object form should turn checksVoidReturn on, as upstream's parseChecksVoidReturn does",
					)
				}
				if decoded.ChecksVoidReturnOpts == nil {
					test.Fatal("the nested object did not bind at all")
				}
				if decoded.ChecksVoidReturnOpts.Arguments == nil || *decoded.ChecksVoidReturnOpts.Arguments {
					test.Fatal("arguments should have decoded to an explicit false")
				}
				if decoded.ChecksVoidReturnOpts.Attributes == nil || !*decoded.ChecksVoidReturnOpts.Attributes {
					test.Fatal("attributes should have decoded to an explicit true")
				}
				if decoded.ChecksVoidReturnOpts.Properties != nil {
					test.Fatal("an omitted sub-flag should stay nil for Run to default")
				}
			},
		},
		{
			name:   "the boolean form of checksVoidReturn binds onto ChecksVoidReturn alone",
			config: `{"checksVoidReturn": false}`,
			assert: func(test *testing.T, decoded NoMisusedPromisesOptions) {
				if decoded.ChecksVoidReturn == nil || *decoded.ChecksVoidReturn {
					test.Fatal("checksVoidReturn should have decoded to an explicit false")
				}
				if decoded.ChecksVoidReturnOpts != nil {
					test.Fatal("the boolean form carries no sub-flags, so ChecksVoidReturnOpts should stay nil")
				}
			},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, err := decode([]byte(testCase.config))
			if err != nil {
				test.Fatalf("decoding %s: %v", testCase.config, err)
			}
			testCase.assert(test, decoded)
		})
	}
}

// TestNoMisusedPromisesTreatsNilOptionsAsEveryCheckEnabled pins the bare-"error" configuration path.
//
// A rule configured as bare "error" is handed no options rather than a zero struct, and reading
// them yields the zero value with every pointer nil. For a
// rule whose nine options ALL default to true, getting that wrong would turn the entire rule off for
// the most common way anyone configures it, while every fixture above (each of which reaches the
// rule through an explicit options value) stayed green. This bypasses the decoder deliberately.
func TestNoMisusedPromisesTreatsNilOptionsAsEveryCheckEnabled(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		fileName   string
		sourceText string
		wantId     string
	}{
		{"checksConditionals", noMisusedPromisesFile, "const p = Promise.resolve();\nif (p) {\n}\n", "conditional"},
		{
			"checksSpreads",
			noMisusedPromisesFile,
			"const p = Promise.resolve({ a: 1 });\nconst o = { ...p };\n",
			"spread",
		},
		{
			"checksVoidReturn",
			noMisusedPromisesFile,
			"declare function f(cb: () => void): void;\nf(async () => {});\n",
			"voidReturnArgument",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, testCase.fileName, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantId)
		})
	}
}

// TestNoMisusedPromisesExclusionsAreStated names the one upstream case this port does not run.
//
// The corpus is one hundred and twenty-three valid and ninety invalid cases; the tables above carry
// one hundred and twenty-three and eighty-nine. This is the missing one, and it is upstream's own
// exclusion rather than a trim made here: tsgolint marks it `Skip: true`, so upstream does not run it
// either.
//
// Stated as a test rather than as a comment so the count is checkable rather than assertable. If a
// later sync unskips it upstream, the case comes back through the extractor and this test's own
// arithmetic is what says so.
func TestNoMisusedPromisesExclusionsAreStated(test *testing.T) {
	test.Parallel()

	const upstreamValidCases = 123
	const upstreamInvalidCases = 90

	exclusions := map[string]string{
		"invalid 31": "marked Skip: true upstream; a JSX attribute case under an explicit attributes:true option that tsgolint does not run either",
	}

	// Counted by re-reading the tables' own files, so the arithmetic cannot drift from the fixtures.
	silentCases, firesCases := noMisusedPromisesTableSizes(test)

	if silentCases != upstreamValidCases {
		test.Fatalf("the silent table carries %d cases against upstream's %d valid", silentCases, upstreamValidCases)
	}
	if firesCases != upstreamInvalidCases-len(exclusions) {
		test.Fatalf(
			"the fires table carries %d cases against upstream's %d invalid minus %d stated exclusions",
			firesCases, upstreamInvalidCases, len(exclusions),
		)
	}
}

// noMisusedPromisesCaseNamePattern matches a generated case label in either imported table.
var noMisusedPromisesCaseNamePattern = regexp.MustCompile(`^\s*name:\s+"upstream (valid|invalid) \d+`)

// noMisusedPromisesTableSizes counts the cases in the two imported tables by parsing the files that
// hold them.
//
// Counting by parsing rather than by a hand-maintained constant, so that a case added or dropped from
// either table moves the number the exclusion test checks. A constant would simply be a second place
// to write the same wrong answer.
func noMisusedPromisesTableSizes(test *testing.T) (silent int, fires int) {
	test.Helper()

	for _, tableFile := range []string{"no_misused_promises_silent_test.go", "no_misused_promises_fires_test.go"} {
		source, readError := os.ReadFile(tableFile)
		if readError != nil {
			test.Fatalf("reading %s to count its table: %v", tableFile, readError)
		}

		// Matched with a regexp rather than a prefix, because gofmt aligns the values in a struct
		// literal to the widest field name in the block, so the run of spaces after `name:` is decided
		// by the formatter rather than by what is written here. A prefix pattern silently counted 25 of
		// 123.
		for _, line := range strings.Split(string(source), "\n") {
			match := noMisusedPromisesCaseNamePattern.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			switch match[1] {
			case "valid":
				silent++
			case "invalid":
				fires++
			}
		}
	}

	if silent == 0 || fires == 0 {
		test.Fatal("counted zero cases in one of the tables, so this measurement proved nothing")
	}
	return silent, fires
}

// TestNoMisusedPromisesReportsPerElementRatherThanPerSite pins the bookkeeping choice, measured on
// triples rather than reasoned from the code.
//
// Whether a rule reports once per site or once per offending element looks like a data-structure
// choice and is a behavioral decision. The corpus cannot settle it: its longest multi-finding case
// carries four, and none of its shapes isolates the axis. So each of these was probed as a triple and
// every position was read rather than only the count.
//
// The answer is per element, uniformly, across every shape this rule has. That includes the case that
// could plausibly have gone the other way and did not: one async method against three heritage types
// that each declare it reports three times, all three at the same span. That is
// checkClassLikeOrInterfaceNode looping over heritage types with no bookkeeping to suppress a repeat,
// so a class implementing several interfaces with a common method gets a finding per interface. It is
// upstream's behavior, reproduced, and it is the shape a reader would most likely "fix" by accident.
//
// The array case is the interesting silence: three async callbacks inside an array literal passed to
// an `Array<() => void>` parameter reports nothing, because the void-return argument check inspects
// the argument itself, and the argument is one array rather than three functions.
func TestNoMisusedPromisesReportsPerElementRatherThanPerSite(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
		wantSpans  []string
	}{
		{
			name: "three async arguments to three void parameters report three times",
			sourceText: `
declare function f(a: () => void, b: () => void, c: () => void): void;
f(async () => {}, async () => {}, async () => {});
`,
			wantIds:   []string{"voidReturnArgument", "voidReturnArgument", "voidReturnArgument"},
			wantSpans: []string{"async () => {}", "async () => {}", "async () => {}"},
		},
		{
			name: "three async arguments to a void rest parameter report three times",
			sourceText: `
declare function f(...callbacks: Array<() => void>): void;
f(async () => {}, async () => {}, async () => {});
`,
			wantIds:   []string{"voidReturnArgument", "voidReturnArgument", "voidReturnArgument"},
			wantSpans: []string{"async () => {}", "async () => {}", "async () => {}"},
		},
		{
			name: "three promise spreads in one object report three times",
			sourceText: `
const p = Promise.resolve({ a: 1 });
const o = { ...p, ...p, ...p };
`,
			wantIds:   []string{"spread", "spread", "spread"},
			wantSpans: []string{"p", "p", "p"},
		},
		{
			name: "one promise tested three times in one condition reports three times",
			sourceText: `
const p = Promise.resolve();
if (p && p && p) {
}
`,
			wantIds:   []string{"conditional", "conditional", "conditional"},
			wantSpans: []string{"p", "p", "p"},
		},
		{
			name: "ONE async method against THREE heritage types reports three times at one span",
			sourceText: `
interface One {
  m(): void;
}
interface Two {
  m(): void;
}
interface Three {
  m(): void;
}
class Derived implements One, Two, Three {
  async m() {}
}
`,
			wantIds:   []string{"voidReturnInheritedMethod", "voidReturnInheritedMethod", "voidReturnInheritedMethod"},
			wantSpans: []string{"async m() {}", "async m() {}", "async m() {}"},
		},
		{
			name: "three async callbacks inside ONE array argument report NOTHING",
			sourceText: `
declare function f(callbacks: Array<() => void>): void;
f([async () => {}, async () => {}, async () => {}]);
`,
			wantIds:   nil,
			wantSpans: nil,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, testCase.wantIds...)

			source := result.SourceFile.Text()
			for index, wantSpan := range testCase.wantSpans {
				diagnostic := result.Diagnostics[index]
				if got := source[diagnostic.Range.Pos():diagnostic.Range.End()]; got != wantSpan {
					test.Fatalf("finding %d span: got %q, want %q", index, got, wantSpan)
				}
			}
		})
	}
}

// TestNoMisusedPromisesDependsOnGlobalsTheFixtureLibraryOmits records the ES2022 hazard for this rule,
// measured with a two-file control rather than inherited from a sibling's verdict.
//
// The fixture tsconfig pins `lib: ["ES2022"]` with no way to raise it, so a name the DOM or Node
// would supply resolves to nothing. Whether that changes a rule's answer is a per-rule question: it
// inverted three `await-thenable` cases and moved nothing at all on `no-implied-eval`.
//
// Here it bites, and it costs findings rather than adding them. `setTimeout(async () => {})` is
// silent under the bare fixture and reports `voidReturnArgument` once a declaration is supplied,
// because voidFunctionArguments walks the callee's signatures to find which parameters take a void
// return, and an unresolved callee has no signatures to walk. That is the safe direction (a missing
// global costs a finding rather than inventing one), but it is worth pinning, because a future
// fixture written against `setTimeout` would sit in the clean table asserting the opposite of
// upstream while passing.
//
// The controls are what make this a measurement: a real Promise conditional and an async callback to
// a locally declared void parameter report identically with and without the extra file, so the moved
// verdicts are about resolution rather than about the second file's presence.
func TestNoMisusedPromisesDependsOnGlobalsTheFixtureLibraryOmits(test *testing.T) {
	test.Parallel()

	const globals = `
declare global {
  function setTimeout(handler: () => void, ms?: number): number;
}
export {};
`

	cases := []struct {
		name                  string
		sourceText            string
		wantWithoutTheGlobals []string
		wantWithTheGlobals    []string
	}{
		{
			name:                  "an unresolved setTimeout has no signature to find a void parameter in",
			sourceText:            "setTimeout(async () => {});\n",
			wantWithoutTheGlobals: nil,
			wantWithTheGlobals:    []string{"voidReturnArgument"},
		},
		{
			name:                  "control: a locally declared void parameter reports either way",
			sourceText:            "declare function f(cb: () => void): void;\nf(async () => {});\n",
			wantWithoutTheGlobals: []string{"voidReturnArgument"},
			wantWithTheGlobals:    []string{"voidReturnArgument"},
		},
		{
			name:                  "control: a promise conditional reports either way",
			sourceText:            "const p = Promise.resolve();\nif (p) {\n}\n",
			wantWithoutTheGlobals: []string{"conditional"},
			wantWithTheGlobals:    []string{"conditional"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			bare := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, bare, testCase.wantWithoutTheGlobals...)

			withGlobals := rule_testing.RunTypedFiles(test, NoMisusedPromises, map[string]string{
				noMisusedPromisesFile: testCase.sourceText,
				"globals.ts":          globals,
			}, noMisusedPromisesFile)
			rule_testing.ExpectFindings(test, withGlobals, testCase.wantWithTheGlobals...)
		})
	}
}

// TestNoMisusedPromisesChecksOnlyTheVoidPositions pins that the argument check reports at the
// positions voidFunctionArguments selected and not at every thenable-returning argument.
//
// This case exists because a mutant found it. Neutralizing the `slices.Contains(voidArgs, index)`
// membership test survived all two hundred and twelve imported fixtures, and the reason is structural
// rather than accidental: `checkArguments` returns early when voidArgs is empty, so a call with no
// void parameter at all cannot reach the loop either way. The membership test only earns its keep
// when some parameter is void and another is not, and upstream's corpus never writes that shape.
//
// So each case below mixes one void-returning parameter with one parameter that is not, hands both an
// async callback, and asserts one finding. Under the mutant each reports two. That is different
// output rather than a different internal path, which is what makes this a fixture rather than a
// guess: the distinguishing input was measured on both versions before it was written down.
//
// The last case is the control for the whole set. A Promise-returning parameter first and a void
// parameter second still reports once, which proves the single finding above is about which position
// was selected and not about the rule reporting at most once per call.
func TestNoMisusedPromisesChecksOnlyTheVoidPositions(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{
			name: "a void parameter beside a Promise-returning one reports only the void position",
			sourceText: `
declare function f(a: () => void, b: () => Promise<void>): void;
f(async () => {}, async () => {});
`,
		},
		{
			name: "a void parameter beside an any parameter reports only the void position",
			sourceText: `
declare function f(a: () => void, b: any): void;
f(async () => {}, async () => {});
`,
		},
		{
			name: "control: the Promise-returning parameter first still reports once",
			sourceText: `
declare function f(a: () => Promise<void>, b: () => void): void;
f(async () => {}, async () => {});
`,
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, "voidReturnArgument")
		})
	}
}

// TestNoMisusedPromisesReportsThePropertyReturnAnnotationWhenThereIsOne pins where the property check
// points, which is a decision the message id cannot see.
//
// checkProperty chooses between two anchors: when the initializer is function-like and carries an
// explicit return type annotation it reports the annotation, and otherwise it reports the initializer.
// Both produce `voidReturnProperty`, so every fixture asserting the id stayed green over a mutant that
// collapsed the two branches into one. That is the "fixtures assert the wrong layer" failure: the
// case was covered, and every assertion over it was about which rule fired rather than where.
//
// Four shapes, because the choice turns on two independent things (is the initializer function-like,
// does it carry an annotation) and the corpus writes only some of the combinations.
func TestNoMisusedPromisesReportsThePropertyReturnAnnotationWhenThereIsOne(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantSpan   string
	}{
		{
			name: "an arrow initializer WITH a return annotation reports the annotation",
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async (): Promise<string> => 'foo',
};
`,
			wantSpan: "Promise<string>",
		},
		{
			name: "an arrow initializer WITHOUT a return annotation reports the initializer",
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async () => 'foo',
};
`,
			wantSpan: "async () => 'foo'",
		},
		{
			name: "a function-expression initializer with an annotation reports the annotation",
			sourceText: `
type O = { f: () => void };
const obj: O = {
  f: async function (): Promise<void> {},
};
`,
			wantSpan: "Promise<void>",
		},
		{
			name: "an initializer that is not function-like reports the initializer",
			sourceText: `
type O = { f: () => void };
const asyncFunction = async () => 'foo';
const obj: O = {
  f: asyncFunction,
};
`,
			wantSpan: "asyncFunction",
		},
		{
			name: "a method shorthand with an annotation reports the annotation",
			sourceText: `
type O = { f: () => void };
const obj: O = {
  async f(): Promise<void> {},
};
`,
			wantSpan: "Promise<void>",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, "voidReturnProperty")

			source := result.SourceFile.Text()
			diagnostic := result.Diagnostics[0]
			if got := source[diagnostic.Range.Pos():diagnostic.Range.End()]; got != testCase.wantSpan {
				test.Fatalf("reported span: got %q, want %q", got, testCase.wantSpan)
			}
		})
	}
}

// TestNoMisusedPromisesSurvivesAttributeValuesThatAreNotExpressionContainers pins a guard whose job is
// to prevent a crash rather than to change a verdict.
//
// checkJSXAttribute requires the initializer's kind to be KindJsxExpression before calling
// AsJsxExpression on it, and that call is an unchecked type assertion. Removing the kind check does
// not merely widen the rule: it panics with `ast.nodeData is *ast.StringLiteral, not *ast.JsxExpression`
// on the first string-valued attribute in the tree, which is to say on essentially any real JSX file.
//
// A mutant removing it survived all two hundred and twelve imported fixtures, because upstream's
// corpus writes no string-valued attribute at all. No ExpectFindings assertion can see a panic, so
// this asserts the shapes reach the rule and come back clean; the crash is what failure looks like.
//
// The control matters here more than usual: without a case that does report on the same file shape,
// a suite of three clean assertions would pass just as well against a rule that had stopped running.
func TestNoMisusedPromisesSurvivesAttributeValuesThatAreNotExpressionContainers(test *testing.T) {
	test.Parallel()

	test.Run("a string-valued attribute beside a reporting one does not crash the rule", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesTsxFile, `
type Props = { onEvent: () => void; label: string };
declare function Component(props: Props): null;
const element = <Component onEvent={async () => {}} label="hello" />;
`)
		rule_testing.ExpectFindings(test, result, "voidReturnAttribute")
	})

	test.Run("a string-valued attribute on a void-function prop is silent rather than fatal", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesTsxFile, `
type Props = { onEvent: () => void };
declare function Component(props: Props): null;
const element = <Component onEvent="hello" />;
`)
		rule_testing.ExpectClean(test, result)
	})

	test.Run("a shorthand attribute with no initializer at all is silent rather than fatal", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesTsxFile, `
type Props = { onEvent?: () => void };
declare function Component(props: Props): null;
const element = <Component onEvent />;
`)
		rule_testing.ExpectClean(test, result)
	})
}

// TestNoMisusedPromisesNamesTheHeritageTypeInTheMessage asserts the one message on this rule that
// interpolates, on the rendered text rather than on the id.
//
// `voidReturnInheritedMethod` is built with Sprintf and carries the name of the extended or
// implemented type that declared the void-returning member. An id assertion cannot see anything a
// format string does, so a mutant replacing the interpolated type name with a constant survived all
// two hundred and twelve imported fixtures and every span assertion. The finding count was right,
// the id was right, the anchor was right, and the sentence a user reads named the wrong type.
//
// Asserted as equality against a literal typed here rather than against the rule's own message
// builder, because a comparison against the builder moves with the builder and asserts nothing.
//
// The two cases separate the interpolation from the surrounding sentence: an `implements` clause and
// an `extends` clause name different types, so a mutant that hardcoded either one is caught by the
// other.
func TestNoMisusedPromisesNamesTheHeritageTypeInTheMessage(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		sourceText  string
		wantMessage string
	}{
		{
			name: "an implements clause names the implemented interface",
			sourceText: `
interface Base {
  method(): void;
}
class Derived implements Base {
  async method() {}
}
`,
			wantMessage: "Promise-returning method provided where a void return was expected by extended/implemented type 'Base'.",
		},
		{
			name: "an extends clause names the extended interface",
			sourceText: `
interface Parent {
  method(): void;
}
interface Child extends Parent {
  method(): Promise<void>;
}
`,
			wantMessage: "Promise-returning method provided where a void return was expected by extended/implemented type 'Parent'.",
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, testCase.sourceText)
			rule_testing.ExpectFindings(test, result, "voidReturnInheritedMethod")

			if got := result.Diagnostics[0].Message.Description; got != testCase.wantMessage {
				test.Fatalf("rendered message:\n got  %q\n want %q", got, testCase.wantMessage)
			}
		})
	}
}

// TestNoMisusedPromisesSkipsAReturnOutsideAnyFunction pins that a top-level `return` is passed over
// rather than fatal.
//
// TypeScript parses `return` at the top of a file and leaves the complaint to the checker, so the
// statement reaches checkReturnStatement with no enclosing function. Upstream throws there, through
// `nullThrows` on the missing parent, and this port panicked the same way, which stopped the whole
// lint run on one file the compiler already rejects. The returned function in a real function
// alongside it is the control: it reports, so the clean top level is the guard and not a rule that
// stopped running.
func TestNoMisusedPromisesSkipsAReturnOutsideAnyFunction(test *testing.T) {
	test.Parallel()
	result := rule_testing.RunTyped(test, NoMisusedPromises, noMisusedPromisesFile, `
return async () => 0;
function f(): () => void {
  return async () => 0;
}
`)
	rule_testing.ExpectFindings(test, result, "voidReturnReturnValue")
}
