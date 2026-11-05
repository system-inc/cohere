package core

import (
	"encoding/json"
	"strings"
	"testing"

	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// noRestrictedImportsFile is where the fixtures pretend to live.
const noRestrictedImportsFile = "/repository/source/NoRestrictedImports.ts"

// noRestrictedImportsCase is one imported corpus row.
type noRestrictedImportsCase struct {
	sourceText string
	options    NoRestrictedImportsOptions
	wantIds    []string
}

// runNoRestrictedImports drives one case through the rule's own exported decoder.
//
// Through the decoder rather than by handing the rule a struct, because the decoder holds every
// schema constraint upstream expresses declaratively -- eight of them -- plus the compile of every
// user-supplied regular expression and glob group. A fixture bypassing it would leave all of that
// untested while still passing.
func runNoRestrictedImports(test *testing.T, testCase noRestrictedImportsCase) rule_testing.Result {
	test.Helper()

	encoded, err := json.Marshal(testCase.options)
	if err != nil {
		test.Fatalf("could not encode options: %v", err)
	}
	// The struct is upstream's `{paths, patterns}` object form, so the list the config layer hands a
	// list rule is that object as its one element.
	decoded, err := DecodeNoRestrictedImportsOptions([]byte("[" + string(encoded) + "]"))
	if err != nil {
		test.Fatalf("could not decode options for %q: %v", testCase.sourceText, err)
	}
	return rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
		testCase.sourceText, decoded)
}

// TestNoRestrictedImportsIsSilentWithNoConfiguration is the half a corpus of configured cases cannot
// cover.
func TestNoRestrictedImportsIsSilentWithNoConfiguration(test *testing.T) {
	test.Parallel()

	const source = "import fs from 'fs'; export { a } from 'mod'; import * as ns from 'os';"

	unconfigured := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
		source, NoRestrictedImportsOptions{})
	rule_testing.ExpectClean(test, unconfigured)

	// The control. Without it the assertion above passes for a rule that can never report at all.
	configured := runNoRestrictedImports(test, noRestrictedImportsCase{
		sourceText: source,
		options: NoRestrictedImportsOptions{
			Paths: []NoRestrictedImportsPath{{Name: "fs"}, {Name: "mod"}, {Name: "os"}},
		},
	})
	rule_testing.ExpectFindings(test, configured, "path", "path", "path")
}

// TestNoRestrictedImportsEmptyObjectRestrictsNothing pins `[{}]`, and the divergence it keeps.
//
// Kept divergence, ruled on #e06zm4b (#d21war2). ESLint 10.8.1's Linter, measured, reports
// `import b from "undefined"` under `[{}]` and nothing else: its runtime reads the empty object as a
// path entry and keys it by the name it does not have. Here `[{}]` restricts nothing, so the
// module named `undefined` is clean too, and this test fails if that ever drifts back.
func TestNoRestrictedImportsEmptyObjectRestrictsNothing(test *testing.T) {
	test.Parallel()

	decoded, decodeNoRestrictedImportsOptionsError := DecodeNoRestrictedImportsOptions([]byte(`[{}]`))
	if decodeNoRestrictedImportsOptionsError != nil {
		test.Fatalf("decoding [{}]: %v", decodeNoRestrictedImportsOptionsError)
	}
	result := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
		`import a from "foo"; import b from "undefined"; import c from "";`, decoded)
	rule_testing.ExpectClean(test, result)
}

// TestNoRestrictedImportsChecksImportEquals covers the one arm upstream's corpus does not reach.
//
// Upstream added a `TSImportEqualsDeclaration` listener and wrote no case for it, so these
// expectations come from reading its source rather than from replaying a measured verdict, and that
// is stated here rather than left for a reader to infer from the corpus's silence.
//
// Two behaviours are asserted, both of which follow from upstream passing an empty import-name map
// for this form: a whole-module restriction reports, and an `importNames` restriction cannot.
func TestNoRestrictedImportsChecksImportEquals(test *testing.T) {
	test.Parallel()

	cases := []noRestrictedImportsCase{
		{
			sourceText: "import foo = require('bar');",
			options:    NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{{Name: "bar"}}},
			wantIds:    []string{"path"},
		},
		{
			// An importNames restriction has no binding to attach to, so it reports nothing.
			sourceText: "import foo = require('bar');",
			options: NoRestrictedImportsOptions{
				Paths: []NoRestrictedImportsPath{{Name: "bar", ImportNames: []string{"foo"}}},
			},
			wantIds: []string{},
		},
		{
			sourceText: "import foo = require('restricted/thing');",
			options: NoRestrictedImportsOptions{
				Patterns: []NoRestrictedImportsPattern{{Group: []string{"restricted/*"}}},
			},
			wantIds: []string{"patterns"},
		},
		{
			// A namespace alias, not an external module reference. Upstream guards on
			// `TSExternalModuleReference` and so does this.
			sourceText: "namespace bar { export const x = 1; }\nimport foo = bar;",
			options:    NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{{Name: "bar"}}},
			wantIds:    []string{},
		},
	}
	for _, testCase := range cases {
		result := runNoRestrictedImports(test, testCase)
		rule_testing.ExpectFindings(test, result, testCase.wantIds...)
	}
}

// TestNoRestrictedImportsRejectsContradictoryConfiguration covers the decoder's own arms.
//
// Every one of these would otherwise fail silently: an entry with no name matches nothing and reads
// as a clean tree, a pattern with neither a group nor a regex likewise, and an entry pairing a
// restrict-list with an allow-list asks the rule to permit and forbid the same name at once.
func TestNoRestrictedImportsRejectsContradictoryConfiguration(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		configured string
		wantError  bool
	}{
		{"a path with no name", `[{"paths":[{"message":"nope"}]}]`, true},
		{
			"importNames beside allowImportNames",
			`[{"paths":[{"name":"m","importNames":["a"],"allowImportNames":["b"]}]}]`,
			true,
		},
		{"a pattern with neither group nor regex", `[{"patterns":[{"message":"nope"}]}]`, true},
		{"a pattern with both group and regex", `[{"patterns":[{"group":["a"],"regex":"a"}]}]`, true},
		{
			"importNamePattern beside allowImportNamePattern",
			`[{"patterns":[{"group":["a"],"importNamePattern":"^a","allowImportNamePattern":"^b"}]}]`,
			true,
		},
		{"an uncompilable regex", `[{"patterns":[{"regex":"("}]}]`, true},
		{"a bare string, which the config layer never delivers to a list rule", `"fs"`, true},
		{"upstream's variadic paths", `["fs","os"]`, false},
		{"a path object among strings", `["fs",{"name":"os","importNames":["a"]}]`, false},
		{"the object form with a second element", `[{"paths":["fs"]},"os"]`, true},
		{"the object form with an unknown key", `[{"paths":["fs"],"pattern":["os/*"]}]`, true},
		{"the nested workaround spelling", `[["fs","os"]]`, true},
		{"the object form", `[{"paths":["fs"],"patterns":["os/*"]}]`, false},
		{"patterns as bare strings", `[{"patterns":["foo/*","!foo/bar"]}]`, false},
		{"a lookahead regex", `[{"patterns":[{"regex":"foo/(?!bar)"}]}]`, false},
		{"no options at all", ``, false},
		{"the empty object form", `[{}]`, false},
		{"the empty object beside a path, which upstream's schema refuses", `[{},"fs"]`, true},
		{"a path object with no name, which upstream's schema refuses", `[{"message":"nope"}]`, true},
	}
	for _, testCase := range cases {
		_, decodeNoRestrictedImportsOptionsError := DecodeNoRestrictedImportsOptions([]byte(testCase.configured))
		if testCase.wantError && decodeNoRestrictedImportsOptionsError == nil {
			test.Errorf("%s: expected the decoder to refuse %s", testCase.name, testCase.configured)
		}
		if !testCase.wantError && decodeNoRestrictedImportsOptionsError != nil {
			test.Errorf(
				"%s: expected the decoder to accept %s, got %v",
				testCase.name,
				testCase.configured,
				decodeNoRestrictedImportsOptionsError,
			)
		}
	}
}

// TestNoRestrictedImportsFoldsPatternsAsStringsIntoOneGroup pins a shape that is observable rather
// than cosmetic.
//
// Upstream folds a bare `patterns: ["a", "!b"]` into one entry whose group is both, which means the
// `!` un-matches the earlier entry. Decoding them as two separate entries would make the negation
// apply to nothing, and `foo/bar` would report where upstream is silent.
func TestNoRestrictedImportsFoldsPatternsAsStringsIntoOneGroup(test *testing.T) {
	test.Parallel()

	decoded, decodeNoRestrictedImportsOptionsError := DecodeNoRestrictedImportsOptions(
		[]byte(`[{"patterns":["foo/*","!foo/bar"]}]`),
	)
	if decodeNoRestrictedImportsOptionsError != nil {
		test.Fatalf("could not decode: %v", decodeNoRestrictedImportsOptionsError)
	}
	if len(decoded.Patterns) != 1 {
		test.Fatalf("expected the two strings folded into 1 entry, got %d", len(decoded.Patterns))
	}

	// The control that the folding actually changes a verdict, rather than merely a shape.
	excluded := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
		"import { a } from 'foo/bar';", decoded)
	rule_testing.ExpectClean(test, excluded)

	included := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
		"import { a } from 'foo/baz';", decoded)
	rule_testing.ExpectFindings(test, included, "patterns")
}

// TestNoRestrictedImportsTreatsAnAllTypeSpecifierListAsTypeOnly pins the second half of upstream's
// `isTypeOnlyImport`, which its corpus never exercises.
//
// The predicate is a disjunction: the declaration is `import type`, or it has specifiers and every
// one is `type`-marked. Only the first half appears in upstream's corpus, so a mutation that deleted
// the second survived all 263 rows while changing the verdict on ordinary TypeScript.
//
// The expectations here were measured against the installed ESLint 10.8.1 rule under
// `allowTypeImports: true`, not reasoned from the source:
//
//	import { type A, type B } from 'mod';   silent   -- every specifier is type-only
//	import { type A, B } from 'mod';        reports  -- one value specifier is enough
//	import type { A } from 'mod';           silent   -- the declaration-level marker
//	import { A } from 'mod';                reports
//
// The re-export mirror answers identically, which is why both are asserted: they read two different
// fields on two different node kinds here, and a port can easily get one right and the other wrong.
func TestNoRestrictedImportsTreatsAnAllTypeSpecifierListAsTypeOnly(test *testing.T) {
	test.Parallel()

	options := NoRestrictedImportsOptions{
		Paths: []NoRestrictedImportsPath{{Name: "mod", AllowTypeImports: true}},
	}

	cases := []struct {
		source  string
		wantIds []string
	}{
		{"import { type A, type B } from 'mod';", []string{}},
		{"import { type A, B } from 'mod';", []string{"path"}},
		{"import type { A } from 'mod';", []string{}},
		{"import { A } from 'mod';", []string{"path"}},
		{"export { type A, type B } from 'mod';", []string{}},
		{"export { type A, B } from 'mod';", []string{"path"}},
		{"export type { A } from 'mod';", []string{}},
		{"export { A } from 'mod';", []string{"path"}},
		// A default import alongside an all-type named list is not type-only: `default` cannot
		// carry a per-specifier `type` marker, so the statement still imports a value. Measured
		// through the installed build, which reports `path` here. A reading that looked only at the
		// named specifiers would call this type-only and stay silent.
		{"import def, { type A } from 'mod';", []string{"path"}},
		{"import def from 'mod';", []string{"path"}},
	}
	for _, testCase := range cases {
		result := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			testCase.source, options)
		rule_testing.ExpectFindings(test, result, testCase.wantIds...)
	}
}

// TestNoRestrictedImportsAnchorsANamespaceFindingOnTheStar asserts where a finding lands, which no
// fixture comparing message ids can see.
//
// A namespace import and a `export * from` both report under the `everything` family, and upstream
// anchors both on the `*` rather than on the statement. For `export * from 'm'` there is no
// specifier node at all: upstream synthesises the location from `sourceCode.getFirstToken(node, 1)`,
// the token after `export`. Measured through the installed ESLint 10.8.1, which reports at column 8
// on that source, the `*`.
//
// This exists because a mutation that anchored every such finding on the start of the statement
// survived all 263 corpus rows. Every one of them asserts ids, and the ids do not move.
func TestNoRestrictedImportsAnchorsANamespaceFindingOnTheStar(test *testing.T) {
	test.Parallel()

	cases := []struct {
		source   string
		options  NoRestrictedImportsOptions
		wantText string
	}{
		{
			"export * from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "foo", ImportNames: []string{"DisallowedObject"}}}},
			"*",
		},
		{
			"export * as ns from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "foo", ImportNames: []string{"DisallowedObject"}}}},
			"*",
		},
		{
			"import AllowedObject, * as All from 'foo';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "foo", ImportNames: []string{"DisallowedObject"}}}},
			"* as All",
		},
	}
	for _, testCase := range cases {
		result := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			testCase.source, testCase.options)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("%q: expected 1 finding, got %d %v",
				testCase.source, len(result.Diagnostics), result.MessageIds())
		}
		span := result.Diagnostics[0].Range
		reported := testCase.source[span.Pos():span.End()]
		if reported != testCase.wantText {
			test.Errorf("%q: finding anchored on %q, want %q", testCase.source, reported, testCase.wantText)
		}
	}
}

// TestNoRestrictedImportsCaseSensitiveGovernsThePathOnly pins the scope of one option, which the
// corpus happens not to separate.
//
// `caseSensitive` reads as though it governs the whole pattern entry, and it does not: upstream
// applies it when building the path matcher and the `regex`, and builds `importNamePattern` and
// `allowImportNamePattern` with a bare `u` flag regardless. So `importNamePattern: '^Foo'` never
// matches `foo`, whatever `caseSensitive` says.
//
// Measured through the installed ESLint 10.8.1 across the four combinations below, all four of which
// answer the same way with the flag on and off. A mutation compiling those two patterns with `iu`
// survived every one of the 263 corpus rows, because no corpus row pairs a name pattern with a name
// of the other case.
func TestNoRestrictedImportsCaseSensitiveGovernsThePathOnly(test *testing.T) {
	test.Parallel()

	for _, caseSensitive := range []bool{false, true} {
		restricting := NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			{Group: []string{"mod"}, ImportNamePattern: "^Foo", CaseSensitive: caseSensitive}}}

		matching := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			"import { Foo } from 'mod';", restricting)
		rule_testing.ExpectFindings(test, matching, "patternAndImportName")

		// The case that moves if the flag is wrongly applied to the name pattern.
		notMatching := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			"import { foo } from 'mod';", restricting)
		rule_testing.ExpectClean(test, notMatching)

		allowing := NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
			{Group: []string{"mod"}, AllowImportNamePattern: "^Foo", CaseSensitive: caseSensitive}}}

		permitted := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			"import { Foo } from 'mod';", allowing)
		rule_testing.ExpectClean(test, permitted)

		refused := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			"import { foo } from 'mod';", allowing)
		rule_testing.ExpectFindings(test, refused, "allowedImportNamePattern")
	}

	// The control that `caseSensitive` is doing something at all, on the half it does govern. With
	// it off, an uppercase group matches a lowercase path; with it on, it does not. Without this,
	// the assertions above would also hold for a rule that ignored the option entirely.
	insensitive := NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
		{Group: []string{"MOD"}, CaseSensitive: false}}}
	rule_testing.ExpectFindings(test, rule_testing.RunWithOptions(test, NoRestrictedImports,
		noRestrictedImportsFile, "import { a } from 'mod';", insensitive), "patterns")

	sensitive := NoRestrictedImportsOptions{Patterns: []NoRestrictedImportsPattern{
		{Group: []string{"MOD"}, CaseSensitive: true}}}
	rule_testing.ExpectClean(test, rule_testing.RunWithOptions(test, NoRestrictedImports,
		noRestrictedImportsFile, "import { a } from 'mod';", sensitive))
}

// TestNoRestrictedImportsRendersTheWholeMessage asserts the text a reader sees, which every fixture
// above is blind to because they compare message ids and the ids do not move.
//
// Three things vary inside one sentence and each can be wrong independently: the list rendering
// (upstream's `Intl.ListFormat("en-US")`, which is quoted, comma separated, with `and` before the
// last and a serial comma only from three items), the verb agreement, and whether a project's own
// message is appended. A mutation that made the verb always plural survived all 263 corpus rows.
func TestNoRestrictedImportsRendersTheWholeMessage(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name     string
		source   string
		options  NoRestrictedImportsOptions
		contains []string
	}{
		{
			"one name agrees singular",
			"import * as ns from 'mod';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "mod", ImportNames: []string{"foo"}}}},
			[]string{"'foo' from `mod` is restricted"},
		},
		{
			"two names take `and` with no serial comma",
			"import * as ns from 'mod';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "mod", ImportNames: []string{"foo", "bar"}}}},
			[]string{"'foo' and 'bar' from `mod` are restricted"},
		},
		{
			"three names take a serial comma",
			"import * as ns from 'mod';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "mod", ImportNames: []string{"foo", "bar", "baz"}}}},
			[]string{"'foo', 'bar', and 'baz' from `mod` are restricted"},
		},
		{
			"a project's own message is appended",
			"import fs from 'fs';",
			NoRestrictedImportsOptions{Paths: []NoRestrictedImportsPath{
				{Name: "fs", Message: "Use the vfs wrapper instead."}}},
			[]string{"`fs` is restricted from being imported here", "Use the vfs wrapper instead."},
		},
	}
	for _, testCase := range cases {
		result := rule_testing.RunWithOptions(test, NoRestrictedImports, noRestrictedImportsFile,
			testCase.source, testCase.options)
		if len(result.Diagnostics) != 1 {
			test.Fatalf("%s: expected 1 finding, got %d %v",
				testCase.name, len(result.Diagnostics), result.MessageIds())
		}
		rendered := result.Diagnostics[0].Message.Description
		for _, want := range testCase.contains {
			if !strings.Contains(rendered, want) {
				test.Errorf("%s: message %q does not contain %q", testCase.name, rendered, want)
			}
		}
	}
}
