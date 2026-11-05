package typescript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/corpus"
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

	// jsx compiles the subject as .tsx.
	jsx bool

	// environmentDependent marks a case whose answer depends on which @types packages are installed
	// rather than on the rule. Skipped with its reason named, never silently dropped.
	environmentDependent bool

	// withInstalledTypes links a real node_modules beside the fixture so an import resolves.
	//
	// Some cases take their deprecation from a @types package rather than from their own text, and
	// without the link they report nothing, which is indistinguishable from the rule declining them.
	// The link is enough for a named import whose types the fixture reaches directly; it is not
	// enough for anything the harness's own tsconfig excludes, which writes `"types": []` after the
	// setup hook runs and so admits no ambient declarations at all. Those cases are marked
	// harnessBound instead.
	withInstalledTypes bool

	// harnessBound marks a case this test harness cannot express, as opposed to one the rule gets
	// wrong. Skipped with the reason named, and the arm it covers is proven separately.
	harnessBound string

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

// runNoDeprecatedCases runs a block of rows.
func runNoDeprecatedCases(test *testing.T, cases []noDeprecatedCase) {
	test.Helper()
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if testCase.harnessBound != "" {
				test.Skip(testCase.harnessBound)
			}
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

			var decoded any
			if testCase.options != "" {
				parsed, decodeNoDeprecatedOptionsError := DecodeNoDeprecatedOptions(json.RawMessage(testCase.options))
				if decodeNoDeprecatedOptionsError != nil {
					test.Fatalf("the decoder refused %s: %v", testCase.options, decodeNoDeprecatedOptionsError)
				}
				decoded = parsed
			}

			var result rule_testing.Result
			if testCase.withInstalledTypes {
				result = rule_testing.RunTypedFilesWithSetupAndOptions(
					test, NoDeprecated, files, subjectName, decoded, linkInstalledTypesForNoDeprecated(test))
			} else {
				result = rule_testing.RunTypedFilesWithOptions(test, NoDeprecated, files, subjectName, decoded)
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
// against upstream's own declared expectations. 259 of the 264 agreed; the five that did not are
// marked environmentDependent where they appear and carry upstream's declaration instead.
func TestNoDeprecatedAgainstUpstreamOrdinaryCases(test *testing.T) {
	test.Parallel()

	runNoDeprecatedCases(test, noDeprecatedOrdinaryValidCases)
	runNoDeprecatedCases(test, noDeprecatedOrdinaryInvalidCases)
}

// TestNoDeprecatedAgainstNodeTypes covers deprecation that comes from a @types package.
//
// Every case here is environment dependent: whether `fs.exists` or `assert.fail` is marked
// deprecated is a property of the installed @types/node, which is not the version upstream pins.
// They carry upstream's declared expectations and are skipped rather than asserted, because a
// machine with different types would fail them for a reason that says nothing about the rule.
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
			expected: nil,
		},
		{
			name: "valid:79",
			sourceText: "\n" +
				"import { exists } from 'fs';\n" +
				"exists('/foo');\n" +
				"      ",
			options:              "{\"allow\":[{\"from\":\"package\",\"name\":\"exists\",\"package\":\"fs\"}]}",
			environmentDependent: true,
			expected:             nil,
		},
		{
			name: "invalid:75",
			sourceText: "\n" +
				"import assert from 'node:assert';\n" +
				"\n" +
				"assert.fail({}, {});\n" +
				"      ",
			environmentDependent: true,
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
			options:      "{\"allow\":[{\"from\":\"package\",\"name\":\"exists\",\"package\":\"hoge\"}]}",
			harnessBound: "the deprecation is on @types/node's fs.exists, which the harness's \"types\": [] excludes; the allowlist arm this case exercises is proven by the other allow cases, and whether fs.exists is marked deprecated differs between @types/node versions anyway",
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
