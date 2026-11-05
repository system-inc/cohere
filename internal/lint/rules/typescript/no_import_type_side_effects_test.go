package typescript

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// importTypeSideEffectsFile is where the fixtures pretend to live.
const importTypeSideEffectsFile = "/repository/source/ImportTypeSideEffects.ts"

// The corpus is typescript-eslint's own, extracted from
// `packages/eslint-plugin/tests/rules/no-import-type-side-effects.test.ts` by a script rather than
// retyped, and byte-compared against the file afterwards. It carries 11 valid cases and 4 invalid
// ones, and every invalid case ships an `output`, so the repair below is asserted rather than
// eyeballed. Cases added beyond the corpus are marked and say what they cover.
func TestNoImportTypeSideEffectsFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
		fixed  string
	}{
		{name: "a single type specifier", source: "import { type A } from 'mod';", fixed: "import type { A } from 'mod';"},
		{name: "a renamed type specifier", source: "import { type A as AA } from 'mod';", fixed: "import type { A as AA } from 'mod';"},
		{name: "two type specifiers", source: "import { type A, type B } from 'mod';", fixed: "import type { A, B } from 'mod';"},
		{name: "two renamed type specifiers", source: "import { type A as AA, type B as BB } from 'mod';", fixed: "import type { A as AA, B as BB } from 'mod';"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoImportTypeSideEffects, importTypeSideEffectsFile, testCase.source)
			rule_testing.ExpectFindings(test, result, "useTopLevelQualifier")
			rule_testing.ExpectFixedSource(test, result, testCase.fixed)
		})
	}
}

func TestNoImportTypeSideEffectsStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name   string
		source string
	}{
		{name: "a default import", source: "import T from 'mod';"},
		{name: "a namespace import", source: "import * as T from 'mod';"},
		{name: "a plain named import", source: "import { T } from 'mod';"},
		{name: "a top-level type import", source: "import type { T } from 'mod';"},
		{name: "a top-level type import of two names", source: "import type { T, U } from 'mod';"},
		{name: "one inline type beside a plain name", source: "import { type T, U } from 'mod';"},
		{name: "a plain name beside one inline type", source: "import { T, type U } from 'mod';"},
		{name: "a top-level type default import", source: "import type T from 'mod';"},
		{name: "a default binding beside an inline type", source: "import T, { type U } from 'mod';"},
		{name: "a top-level type namespace import", source: "import type * as T from 'mod';"},
		{name: "a bare side-effect import", source: "import 'mod';"},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.Run(test, NoImportTypeSideEffects, importTypeSideEffectsFile, testCase.source))
		})
	}
}

// The cases below are not in the corpus. Each was measured against the installed
// @typescript-eslint 8.67.0 rule before being written down, by driving it through the ESLint Linter
// API, and each covers a decision the corpus leaves unpinned.
func TestNoImportTypeSideEffectsBeyondTheCorpus(test *testing.T) {
	test.Parallel()

	// The empty specifier list is upstream's own early return and no corpus case reaches it. A
	// universal quantifier over an empty list is vacuously true, so a port that drops the guard
	// reports this and offers `import type {} from 'mod';` as the repair.
	test.Run("an empty specifier list stays silent", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, rule_testing.Run(test, NoImportTypeSideEffects,
			importTypeSideEffectsFile, "import {} from 'mod';"))
	})

	// A top-level `type` qualifier with an inline one nested inside it is illegal TypeScript, so the
	// grammar says this shape cannot exist and upstream's `importKind!="type"` selector is written as
	// though it cannot. The parser is not the grammar: it recovers from the illegal source and hands
	// back a type-only clause whose specifiers are also marked type-only, which satisfies the
	// universal below and reaches the report. Measured both ways by removing the guard: with it the
	// input is silent, without it the rule reports and offers `import type type { A } from 'mod';`,
	// which does not parse. Upstream is silent on it, measured at 8.67.0.
	test.Run("a nested type qualifier under a top-level one stays silent", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, rule_testing.Run(test, NoImportTypeSideEffects,
			importTypeSideEffectsFile, "import type { type A } from 'mod';"))
	})

	// The removal runs from the keyword to the imported name, so whitespace anywhere else in the
	// statement survives and the qualifier lands against the `import` keyword rather than the brace.
	fixCases := []struct {
		name   string
		source string
		fixed  string
	}{
		{
			name:   "no space inside the braces",
			source: "import {type A} from 'mod';",
			fixed:  "import type {A} from 'mod';",
		},
		{
			name:   "runs of spaces everywhere",
			source: "import   {   type   A   }   from 'mod';",
			fixed:  "import type   {   A   }   from 'mod';",
		},
		// A name-based removal has to decide which `as` is the keyword and which is the name. The
		// positional span never asks the question, so all four of these are mechanical.
		{
			name:   "the imported name is the word as",
			source: "import { type as } from 'mod';",
			fixed:  "import type { as } from 'mod';",
		},
		{
			name:   "the imported name is as, renamed to as",
			source: "import { type as as as } from 'mod';",
			fixed:  "import type { as as as } from 'mod';",
		},
		{
			name:   "the imported name is the word type",
			source: "import { type type } from 'mod';",
			fixed:  "import type { type } from 'mod';",
		},
		{
			name:   "the imported name is the word default",
			source: "import { type default as A } from 'mod';",
			fixed:  "import type { default as A } from 'mod';",
		},
		// A string-literal module export name has no identifier to match on at all.
		{
			name:   "a string-literal module export name",
			source: "import { type 'a' as A } from 'mod';",
			fixed:  "import type { 'a' as A } from 'mod';",
		},
		{
			name:   "the specifiers wrap across lines",
			source: "import {\n\ttype A,\n\ttype B,\n} from 'mod';",
			fixed:  "import type {\n\tA,\n\tB,\n} from 'mod';",
		},
	}

	for _, testCase := range fixCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoImportTypeSideEffects,
				importTypeSideEffectsFile, testCase.source)
			rule_testing.ExpectFindings(test, result, "useTopLevelQualifier")
			rule_testing.ExpectFixedSource(test, result, testCase.fixed)
		})
	}
}

// The span is the whole import declaration, which upstream states as column 1 through the statement's
// last character, and which no message-id assertion can see. Asserted against a literal rather than
// against the rule's own constant, because a constant compared to itself moves under mutation.
func TestNoImportTypeSideEffectsSpansTheDeclaration(test *testing.T) {
	test.Parallel()

	const source = "const before = 1;\nimport { type A, type B } from 'mod';\n"
	result := rule_testing.Run(test, NoImportTypeSideEffects, importTypeSideEffectsFile, source)
	rule_testing.ExpectFindings(test, result, "useTopLevelQualifier")

	reported := source[result.Diagnostics[0].Range.Pos():result.Diagnostics[0].Range.End()]
	if reported != "import { type A, type B } from 'mod';" {
		test.Fatalf("reported span was %q", reported)
	}
}

// The message carries no format verbs, so there is nothing to render and the assertion is on the
// value itself. Both fields, because the description is the half a reader acts on.
func TestNoImportTypeSideEffectsMessage(test *testing.T) {
	test.Parallel()

	if messageUseTopLevelQualifier.Id != "useTopLevelQualifier" {
		test.Fatalf("message id was %q", messageUseTopLevelQualifier.Id)
	}
	if !strings.HasPrefix(messageUseTopLevelQualifier.Description,
		"Every name in this import carries its own inline") {
		test.Fatalf("message description was %q", messageUseTopLevelQualifier.Description)
	}
}

// The rule takes no options, so it must survive being handed nil the way a bare "error" configuration
// hands it. Bypasses the decoder entirely, which every fixture above reaches the rule through.
func TestNoImportTypeSideEffectsTakesNoOptions(test *testing.T) {
	test.Parallel()

	result := rule_testing.RunWithOptions(test, NoImportTypeSideEffects,
		importTypeSideEffectsFile, "import { type A } from 'mod';", nil)
	rule_testing.ExpectFindings(test, result, "useTopLevelQualifier")

	if NoImportTypeSideEffects.NeedsTypeChecker {
		test.Fatal("the rule declares the type checker and decides nothing that needs it")
	}
	var _ rule.Rule = NoImportTypeSideEffects
}
