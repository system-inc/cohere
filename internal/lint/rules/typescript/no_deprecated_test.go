package typescript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/corpus"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// noDeprecatedFinding is one expected report: which message, on what name, with what reason.
//
// The reason is asserted rather than only the id. 26 of upstream's cases carry a reason, and a rule
// that reported the right node with the wrong reason would pass an id-only assertion while telling
// every reader of the output something false.
type noDeprecatedFinding struct {
	messageId string
	name      string
	reason    string
}

// noDeprecatedCase is one row of the imported corpus.
type noDeprecatedCase struct {
	name       string
	sourceText string
	options    string

	// withDeprecatedFixture adds upstream's shared `deprecated.ts` beside the subject file. 51 cases
	// need it, and they are the ones that exercise the alias chain.
	withDeprecatedFixture bool

	// withNodeTypes adds noDeprecatedNodeTypes beside the subject: the cases whose deprecation upstream
	// reads off @types/node read it off this pinned subset instead, the same on every machine.
	withNodeTypes bool

	// jsx compiles the subject as .tsx.
	jsx bool

	// environmentDependent marks a case whose answer depends on which @types packages are installed
	// rather than on the rule. Skipped with its reason named, never silently dropped.
	environmentDependent bool

	// withInstalledTypes links a real node_modules beside the fixture so an import resolves.
	//
	// Some cases take their deprecation from a @types package rather than from their own text, and
	// without the link they report nothing, which is indistinguishable from the rule declining them.
	// The link is enough for a named import whose types the fixture reaches directly; it admits no
	// ambient declarations, since the harness's tsconfig says `"types": []`. A case that needs those
	// pins them in its own program instead, withNodeTypes or withReactTypes.
	withInstalledTypes bool

	// withReactTypes adds noDeprecatedReactTypes beside the subject, the @types/react declarations
	// an intrinsic element's attributes come from, pinned.
	withReactTypes bool

	// asScript builds the program without `"moduleDetection": "force"`, so a subject with no import
	// or export is a script, as it is in upstream's fixture project, and its `declare namespace JSX`
	// is global rather than local to a module.
	asScript bool

	expected []noDeprecatedFinding
}

// noDeprecatedSharedFixture is upstream's `tests/fixtures/deprecated.ts`, verbatim.
//
// Copied rather than summarized because the three tag positions in it are the whole point: on the
// declaration, on an export specifier over an untagged declaration, and on one overload of several.
const noDeprecatedSharedFixture = "" +
	"/** @deprecated */\n" +
	"export class DeprecatedClass {\n" +
	"  /** @deprecated */\n" +
	"  foo: string = '';\n" +
	"}\n" +
	"/** @deprecated */\n" +
	"export const deprecatedVariable = 1;\n" +
	"/** @deprecated */\n" +
	"export function deprecatedFunction(): void {}\n" +
	"class NormalClass {}\n" +
	"const normalVariable = 1;\n" +
	"function normalFunction(): void;\n" +
	"function normalFunction(arg: string): void;\n" +
	"function normalFunction(arg?: string): void {}\n" +
	"function deprecatedFunctionWithOverloads(): void;\n" +
	"/** @deprecated */\n" +
	"function deprecatedFunctionWithOverloads(arg: string): void;\n" +
	"function deprecatedFunctionWithOverloads(arg?: string): void {}\n" +
	"export class ClassWithDeprecatedConstructor {\n" +
	"  constructor();\n" +
	"  /** @deprecated */\n" +
	"  constructor(arg: string);\n" +
	"  constructor(arg?: string) {}\n" +
	"}\n" +
	"export {\n" +
	"  /** @deprecated */\n" +
	"  NormalClass,\n" +
	"  /** @deprecated */\n" +
	"  normalVariable,\n" +
	"  /** @deprecated */\n" +
	"  normalFunction,\n" +
	"  deprecatedFunctionWithOverloads,\n" +
	"  /** @deprecated Reason */\n" +
	"  deprecatedFunctionWithOverloads as reexportedDeprecatedFunctionWithOverloads,\n" +
	"  /** @deprecated Reason */\n" +
	"  ClassWithDeprecatedConstructor as ReexportedClassWithDeprecatedConstructor,\n" +
	"};\n" +
	"\n" +
	"/** @deprecated Reason */\n" +
	"export type T = { a: string };\n" +
	"\n" +
	"export type U = { b: string };\n" +
	"\n" +
	"/** @deprecated */\n" +
	"export default {\n" +
	"  foo: 1,\n" +
	"};\n"

// noDeprecatedNodeTypes is the part of @types/node that upstream's node cases read, pinned.
//
// Upstream's expectations come from the @types/node its repository installs, and the installed one
// moves: 26.x dropped the deprecated `assert.fail(actual, expected, ...)` overload that invalid:75
// reports. Declaring the modules here, in the shape and with the tag text of the @types/node those
// expectations were written against, makes each row a statement about the rule rather than about
// whatever is installed. A `.d.ts` is a script under `"moduleDetection": "force"`, so its
// `declare module` blocks are ambient, which is also how the `package` allowlist recognizes `fs`.
const noDeprecatedNodeTypes = "" +
	"declare module 'assert' {\n" +
	"  function assert(value: unknown, message?: string | Error): asserts value;\n" +
	"  namespace assert {\n" +
	"    function fail(message?: string | Error): never;\n" +
	"    /** @deprecated since v10.0.0 - use fail([message]) or other assert functions instead. */\n" +
	"    function fail(\n" +
	"      actual: unknown,\n" +
	"      expected: unknown,\n" +
	"      message?: string | Error,\n" +
	"      operator?: string,\n" +
	"      stackStartFn?: Function,\n" +
	"    ): never;\n" +
	"  }\n" +
	"  export = assert;\n" +
	"}\n" +
	"declare module 'node:assert' {\n" +
	"  import assert = require('assert');\n" +
	"  export = assert;\n" +
	"}\n" +
	"declare module 'fs' {\n" +
	"  type PathLike = string;\n" +
	"  export function stat(path: PathLike, callback: (error: Error | null) => void): void;\n" +
	"  export function access(path: PathLike, callback: (error: Error | null) => void): void;\n" +
	"  /**\n" +
	"   * @deprecated Since v1.0.0 - Use {@link stat} or {@link access} instead.\n" +
	"   */\n" +
	"  export function exists(path: PathLike, callback: (exists: boolean) => void): void;\n" +
	"}\n"

// noDeprecatedReactTypes is the part of @types/react that upstream's invalid:143 reads, pinned.
//
// The tag sits where @types/react puts it: on `AriaAttributes`, which `HTMLAttributes` extends, and
// which reaches `<div>` through `DetailedHTMLProps`, an intersection with `ClassAttributes`. So the
// property is found through an inherited interface inside an intersection, the way the real package
// delivers it, rather than declared on the element directly.
const noDeprecatedReactTypes = "" +
	"declare namespace React {\n" +
	"  interface Attributes {\n" +
	"    key?: string | number | null | undefined;\n" +
	"  }\n" +
	"  interface ClassAttributes<T> extends Attributes {\n" +
	"    ref?: ((instance: T | null) => void) | undefined;\n" +
	"  }\n" +
	"  interface AriaAttributes {\n" +
	"    /** @deprecated in ARIA 1.1 */\n" +
	"    'aria-grabbed'?: boolean | 'false' | 'true' | undefined;\n" +
	"    'aria-hidden'?: boolean | 'false' | 'true' | undefined;\n" +
	"  }\n" +
	"  interface HTMLAttributes<T> extends AriaAttributes {\n" +
	"    id?: string | undefined;\n" +
	"  }\n" +
	"  type DetailedHTMLProps<E extends HTMLAttributes<T>, T> = ClassAttributes<T> & E;\n" +
	"}\n" +
	"interface HTMLDivElement {}\n" +
	"declare namespace JSX {\n" +
	"  interface Element {}\n" +
	"  interface IntrinsicElements {\n" +
	"    div: React.DetailedHTMLProps<React.HTMLAttributes<HTMLDivElement>, HTMLDivElement>;\n" +
	"  }\n" +
	"}\n"

// runNoDeprecatedCases runs a block of rows.
func runNoDeprecatedCases(test *testing.T, cases []noDeprecatedCase) {
	test.Helper()
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if testCase.environmentDependent {
				test.Skip("depends on the installed @types packages rather than on the rule; " +
					"upstream's declared expectation is recorded above")
			}

			subjectName := "/repository/source/Subject.ts"
			if testCase.jsx {
				subjectName = "/repository/source/Subject.tsx"
			}
			files := map[string]string{subjectName: testCase.sourceText}
			if testCase.withDeprecatedFixture {
				files["/repository/source/deprecated.ts"] = noDeprecatedSharedFixture
			}
			if testCase.withNodeTypes {
				files["/repository/types/node.d.ts"] = noDeprecatedNodeTypes
			}
			if testCase.withReactTypes {
				files["/repository/types/react.d.ts"] = noDeprecatedReactTypes
			}

			var decoded rule.Given[NoDeprecatedOptions]
			if testCase.options != "" {
				parsed, decodeNoDeprecatedOptionsError := DecodeNoDeprecatedOptions(json.RawMessage(testCase.options))
				if decodeNoDeprecatedOptionsError != nil {
					test.Fatalf("the decoder refused %s: %v", testCase.options, decodeNoDeprecatedOptionsError)
				}
				decoded = rule.Configured(parsed)
			}

			var setups []func(directory string)
			if testCase.withInstalledTypes {
				setups = append(setups, linkInstalledTypesForNoDeprecated(test))
			}
			if testCase.asScript {
				setups = append(setups, writeNoDeprecatedScriptConfiguration(test))
			}
			var result rule_testing.Result
			if len(setups) > 0 {
				result = rule_testing.RunTypedFilesWithSetupGiven(
					test, NoDeprecated, files, subjectName, decoded, func(directory string) {
						for _, setup := range setups {
							setup(directory)
						}
					})
			} else {
				result = rule_testing.RunTypedFilesGiven(test, NoDeprecated, files, subjectName, decoded)
			}

			if len(result.Diagnostics) != len(testCase.expected) {
				test.Fatalf("wanted %d findings, got %d: %s",
					len(testCase.expected), len(result.Diagnostics), describeNoDeprecated(result))
			}
			for index, want := range testCase.expected {
				got := result.Diagnostics[index]
				if got.Message.Id != want.messageId {
					test.Errorf("finding %d: wanted message %s, got %s",
						index, want.messageId, got.Message.Id)
				}
				// The name is interpolated into the description between backticks, and the reason
				// follows it. Asserting the rendered text rather than the pieces is what catches a
				// right-node-wrong-reason report.
				wantText := "`" + want.name + "` is deprecated."
				if want.reason != "" {
					wantText += " " + want.reason
				}
				if got.Message.Description != wantText {
					test.Errorf("finding %d: wanted %q, got %q", index, wantText, got.Message.Description)
				}
			}
		})
	}
}

// linkInstalledTypesForNoDeprecated links a real node_modules into the fixture directory.
//
// The three cases that need it take their deprecation from `@types/react` or `@types/node` rather
// than from their own text. Without the link the types do not resolve, the rule finds nothing, and
// the case reports zero findings, which is indistinguishable from the rule declining them. That is
// the failure mode this whole file is arranged against, so the link is what makes those three
// assertions mean anything.
//
// The packages are ahra's installed ones, so the case skips where the ahra corpus is not set, and the gate
// names it as not covered (#sycrdr6). Where it is set, a missing @types/react fails: the corpus is there and
// the case would otherwise read nothing.
func linkInstalledTypesForNoDeprecated(test *testing.T) func(string) {
	test.Helper()
	return func(directory string) {
		source := corpus.Ahra.Path(test, "node_modules")
		if _, statError := os.Stat(filepath.Join(source, "@types", "react")); statError != nil {
			test.Fatalf("this case reads its deprecation from @types/react, which the ahra corpus at %s does "+
				"not have installed: %v", source, statError)
		}
		if symlinkError := os.Symlink(source, filepath.Join(directory, "node_modules")); symlinkError != nil {
			test.Fatalf("linking node_modules into the fixture: %v", symlinkError)
		}
	}
}

// writeNoDeprecatedScriptConfiguration writes the harness's tsconfig less `"moduleDetection": "force"`.
//
// The harness keeps a tsconfig a setup hook wrote, so this is how one case reaches a program whose
// subject is a script. Everything else is the harness's own, `"types": []` included, so the case
// still sees nothing from the machine.
func writeNoDeprecatedScriptConfiguration(test *testing.T) func(string) {
	test.Helper()
	return func(directory string) {
		const configuration = `{
	"compilerOptions": {
		"strict": true,
		"target": "ES2022",
		"lib": ["ES2022"],
		"types": []
	},
	"include": ["**/*.ts", "**/*.tsx"]
}`
		configurationPath := filepath.Join(directory, "tsconfig.json")
		if writeError := os.WriteFile(configurationPath, []byte(configuration), 0o644); writeError != nil {
			test.Fatalf("writing the script tsconfig: %v", writeError)
		}
	}
}

// describeNoDeprecated renders a result for a failure message.
func describeNoDeprecated(result rule_testing.Result) string {
	if len(result.Diagnostics) == 0 {
		return "(no findings)"
	}
	var builder strings.Builder
	for index, diagnostic := range result.Diagnostics {
		if index > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(diagnostic.Message.Id)
		builder.WriteString(": ")
		builder.WriteString(diagnostic.Message.Description)
	}
	return builder.String()
}

// TestNoDeprecatedAgainstUpstreamOrdinaryCases is the single-file part of upstream's corpus.
//
// Every expectation here was produced by replaying the case through the installed 8.67.0 build with
// a real type checker, one program per case so no two cases share an interned type, then checked
// against upstream's own declared expectations. 259 of the 264 agreed; the five that did not carry
// upstream's declaration instead. Three of them, valid:79, invalid:75 and invalid:144, now run on the
// declarations or program mode they needed pinned; the other two are marked environmentDependent.
func TestNoDeprecatedAgainstUpstreamOrdinaryCases(test *testing.T) {
	test.Parallel()

	runNoDeprecatedCases(test, noDeprecatedOrdinaryValidCases)
	runNoDeprecatedCases(test, noDeprecatedOrdinaryInvalidCases)
}

// TestNoDeprecatedAgainstNodeTypes covers deprecation that comes from a @types package.
//
// Whether `fs.exists` or `assert.fail` is marked deprecated is a property of the @types/node a
// machine has installed, so these rows read noDeprecatedNodeTypes instead and assert upstream's
// declared expectations on every machine. valid:52 and invalid:75 are the same call through two
// overloads, so a rule that asked the merged symbol rather than the resolved signature fails the
// first, and valid:79 and invalid:146 are the same call under two allowlists, so a `package`
// specifier that matched any package, or none, fails one of them.
func TestNoDeprecatedAgainstNodeTypes(test *testing.T) {
	test.Parallel()

	cases := []noDeprecatedCase{
		{
			name: "valid:52",
			sourceText: "\n" +
				"import assert from 'node:assert';\n" +
				"\n" +
				"assert.fail('');\n" +
				"    ",
			withNodeTypes: true,
			expected:      nil,
		},
		{
			name: "valid:79",
			sourceText: "\n" +
				"import { exists } from 'fs';\n" +
				"exists('/foo');\n" +
				"      ",
			options:       "{\"allow\":[{\"from\":\"package\",\"name\":\"exists\",\"package\":\"fs\"}]}",
			withNodeTypes: true,
			expected:      nil,
		},
		{
			name: "invalid:75",
			sourceText: "\n" +
				"import assert from 'node:assert';\n" +
				"\n" +
				"assert.fail({}, {});\n" +
				"      ",
			withNodeTypes: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "fail",
					reason:    "since v10.0.0 - use fail([message]) or other assert functions instead.",
				},
			},
		},
		{
			name: "invalid:146",
			sourceText: "\n" +
				"import { exists } from 'fs';\n" +
				"exists('/foo');\n" +
				"      ",
			options:       "{\"allow\":[{\"from\":\"package\",\"name\":\"exists\",\"package\":\"hoge\"}]}",
			withNodeTypes: true,
			expected: []noDeprecatedFinding{
				{
					messageId: "deprecatedWithReason",
					name:      "exists",
					reason:    "Since v1.0.0 - Use {@link stat} or {@link access} instead.",
				},
			},
		},
	}
	runNoDeprecatedCases(test, cases)
}

// TestNoDeprecatedFiresAndStaysQuiet is the fixture pair, through the shared harness assertions.
//
// The corpus blocks above compare findings themselves, because they assert the rendered message and
// not only the id, and 26 of upstream's cases make that reason text part of the contract. This pair
// exists alongside them so the two claims a rule has to support, that it can fire and that it can
// stay silent, are made in the form the registry guard reads.
//
// The two rows are the same declaration used two ways, so a rule that reported on everything would
// fail the second and a rule that reported on nothing would fail the first. Neither can be passed by
// accident.
func TestNoDeprecatedFiresAndStaysQuiet(test *testing.T) {
	test.Parallel()

	const source = "/** @deprecated Use b instead. */\n" +
		"declare function a(): void;\n" +
		"declare function b(): void;\n"

	test.Run("fires on a use of the deprecated declaration", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoDeprecated, "/repository/source/Subject.ts",
			source+"a();\n")
		rule_testing.ExpectFindings(test, result, "deprecatedWithReason")
	})

	test.Run("stays quiet on a use of the undeprecated one", func(test *testing.T) {
		test.Parallel()
		result := rule_testing.RunTyped(test, NoDeprecated, "/repository/source/Subject.ts",
			source+"b();\n")
		rule_testing.ExpectClean(test, result)
	})

	test.Run("stays quiet on the declaration itself", func(test *testing.T) {
		test.Parallel()
		// The declaration site is not a use, so a rule keyed on the tag rather than on the
		// reference would report here and this is what catches it.
		result := rule_testing.RunTyped(test, NoDeprecated, "/repository/source/Subject.ts", source)
		rule_testing.ExpectClean(test, result)
	})

	test.Run("declines a file with no type checker", func(test *testing.T) {
		test.Parallel()
		// NeedsTypeChecker keeps this unreachable through registration, but the harness path and
		// the registry's crash corpus both build a Context by hand. The rule panicked on exactly
		// this before the guard in Run was written, so the decline is pinned here.
		result := rule_testing.Run(test, NoDeprecated, "/repository/source/Subject.ts", source+"a();\n")
		rule_testing.ExpectClean(test, result)
	})
}

// TestNoDeprecatedRendersALinkInTheReasonAsUpstreamDoes pins the reason text around an inline link.
//
// Each row's want is what TypeScript 6.0.3's `getJsDocTags` and `displayPartsToString` printed for the
// same declarations, which is the text upstream's message carries. The rule printed every one of these
// with the link dropped, `Use  instead.`, and nothing ran that could say so until invalid:146 did. The
// rows in pairs differ only in whether the name resolves, because that is what upstream's spacing turns
// on.
func TestNoDeprecatedRendersALinkInTheReasonAsUpstreamDoes(test *testing.T) {
	test.Parallel()

	rows := []struct {
		link string
		want string
	}{
		{link: "{@link stat}", want: "{@link stat}"},
		{link: "{@link nope}", want: "{@link nope }"},
		{link: "{@link stat the stat}", want: "{@link statthe stat}"},
		{link: "{@link nope some text}", want: "{@link nope some text}"},
		{link: "{@link stat|label}", want: "{@link statlabel}"},
		{link: "{@link nope|label}", want: "{@link nope label}"},
		{link: "{@linkcode stat}", want: "{@linkcode stat}"},
		{link: "{@linkplain stat}", want: "{@linkplain stat}"},
		{link: "{@link https://example.com}", want: "{@link https://example.com}"},
		{link: "{@link https://example.com|site}", want: "{@link https://example.com site}"},
		{link: "{@link stat()}", want: "{@link stat()}"},
		{link: "{@link nope()}", want: "{@link nope()}"},
		{link: "{@link Thing.member}", want: "{@link Thing.member}"},
		{link: "{@link Thing.missing}", want: "{@link Thing.missing }"},
		{link: "{@link}", want: "{@link }"},
		{link: "{@link   stat   }", want: "{@link stat}"},
	}
	cases := make([]noDeprecatedCase, 0, len(rows))
	for _, row := range rows {
		cases = append(cases, noDeprecatedCase{
			name: row.link,
			sourceText: "declare function stat(): void;\n" +
				"declare class Thing { member: number }\n" +
				"/** @deprecated Use " + row.link + " instead. */\n" +
				"declare function target(): void;\n" +
				"target();\n",
			expected: []noDeprecatedFinding{
				{messageId: "deprecatedWithReason", name: "target", reason: "Use " + row.want + " instead."},
			},
		})
	}
	runNoDeprecatedCases(test, cases)
}
