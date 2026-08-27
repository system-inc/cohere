package nexus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

const translationsConfig = `{
    "compilerOptions": {
        "target": "ES2022",
        "module": "esnext",
        "moduleResolution": "bundler",
        "strict": true,
        "noEmit": true
    },
    "include": ["./**/*.ts"]
}`

// runOnTranslations lays a translations directory on disk, builds a real program over it, and runs
// the rule against one file in it.
//
// This rule is the first that reads a second file, so unlike the syntax-only majority it cannot be
// exercised by parsing one string: the sibling en.ts has to actually be in the program, because
// reaching it through the program instead of through the filesystem is the whole porting decision.
// A fixture that faked the English side would be testing the comparison while skipping the part
// most likely to be wrong.
func runOnTranslations(t *testing.T, files map[string]string, subjectRelativePath string) rule_testing.Result {
	t.Helper()

	directory := t.TempDir()
	files["tsconfig.json"] = translationsConfig
	for name, contents := range files {
		full := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}

	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory})
	if err != nil {
		t.Fatalf("building the program: %v", err)
	}

	subjectPath := filepath.Join(directory, subjectRelativePath)
	var subject *ast.SourceFile
	for _, sourceFile := range graph.ProjectFiles() {
		if sourceFile.FileName() == filepath.ToSlash(subjectPath) {
			subject = sourceFile
			break
		}
	}
	if subject == nil {
		t.Fatalf("the program does not hold %s", subjectPath)
	}

	var diagnostics []rule.Diagnostic
	context := rule.Context{
		SourceFile: subject,
		Program:    graph.Program,
		Report: func(diagnostic rule.Diagnostic) {
			diagnostic.RuleName = LocalizationNoUntranslatedValue.Name
			diagnostics = append(diagnostics, diagnostic)
		},
	}

	listeners := LocalizationNoUntranslatedValue.Run(context, nil)
	if listeners != nil {
		walkForTest(subject.AsNode(), listeners)
	}
	return rule_testing.Result{Diagnostics: diagnostics, SourceFile: subject}
}

func walkForTest(node *ast.Node, listeners rule.Listeners) {
	if node == nil {
		return
	}
	if listener, isListening := listeners[node.Kind]; isListening {
		listener(node)
	}
	node.ForEachChild(func(child *ast.Node) bool {
		walkForTest(child, listeners)
		return false
	})
}

const englishTranslations = `export default {
    Greeting: 'Hello there',
    Farewell: 'Goodbye now',
    Ok: 'OK',
    IntentionallyBlank: '',
    Nested: {
        Title: 'Welcome home',
        Deep: {
            Label: 'Continue shopping',
        },
    },
};
`

func TestLocalizationNoUntranslatedValueFires(t *testing.T) {
	cases := []struct {
		name    string
		spanish string
		wantIds []string
	}{
		{
			"an empty value where English has content",
			"export default {\n    Greeting: '',\n};\n",
			[]string{"missingTranslation"},
		},
		{
			"a value copied from English",
			"export default {\n    Greeting: 'Hello there',\n};\n",
			[]string{"identicalToSource"},
		},
		{
			"a whitespace-only value counts as empty",
			"export default {\n    Greeting: '   ',\n};\n",
			[]string{"missingTranslation"},
		},
		{
			// Nesting is the shape real translation files take, and a walk that stopped at the top
			// level would silently check only the shallowest keys.
			"a copied value nested one level deep",
			"export default {\n    Nested: {\n        Title: 'Welcome home',\n    },\n};\n",
			[]string{"identicalToSource"},
		},
		{
			"a copied value nested two levels deep",
			"export default {\n    Nested: {\n        Deep: {\n            Label: 'Continue shopping',\n        },\n    },\n};\n",
			[]string{"identicalToSource"},
		},
		{
			// Both defects in one file, in source order, because one finding must not hide another.
			"an empty value and a copied value together",
			"export default {\n    Greeting: '',\n    Farewell: 'Goodbye now',\n};\n",
			[]string{"missingTranslation", "identicalToSource"},
		},
		{
			"a quoted key is read like a bare one",
			"export default {\n    'Greeting': 'Hello there',\n};\n",
			[]string{"identicalToSource"},
		},
		{
			// The `satisfies` and `as` wrappers are type-only, so the object underneath is still
			// the translations. A port that failed to unwrap them would decline these files
			// silently, which is the failure that looks exactly like a clean locale.
			"a satisfies assertion is unwrapped",
			"export default {\n    Greeting: 'Hello there',\n} satisfies Record<string, unknown>;\n",
			[]string{"identicalToSource"},
		},
		{
			"an as assertion is unwrapped",
			"export default {\n    Greeting: 'Hello there',\n} as Record<string, unknown>;\n",
			[]string{"identicalToSource"},
		},
		{
			"an identifier reference is followed to its const",
			"const Spanish = {\n    Greeting: 'Hello there',\n} as const;\n\nexport default Spanish;\n",
			[]string{"identicalToSource"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := runOnTranslations(t, map[string]string{
				"translations/en.ts": englishTranslations,
				"translations/es.ts": testCase.spanish,
			}, "translations/es.ts")
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

func TestLocalizationNoUntranslatedValueStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		spanish string
	}{
		{"a real translation", "export default {\n    Greeting: 'Hola que tal',\n};\n"},
		{"real translations at every depth", "export default {\n    Greeting: 'Hola que tal',\n    Nested: {\n        Title: 'Bienvenido a casa',\n        Deep: {\n            Label: 'Seguir comprando',\n        },\n    },\n};\n"},
		// Short strings are genuinely identical in many languages. Without the length floor every
		// 'OK' and 'Email' in the tree reports, and a rule that cries wolf gets switched off.
		{"a short value identical to English", "export default {\n    Ok: 'OK',\n};\n"},
		// A key English does not have has no comparison basis, so it is not this rule's business
		// whatever it holds.
		{"a key absent from English", "export default {\n    LocaleOnly: 'Solo en espanol',\n};\n"},
		{"an empty value for a key absent from English", "export default {\n    LocaleOnly: '',\n};\n"},
		// English is deliberately blank for this key, so a blank locale value is correct rather
		// than missing. Without the "English has content" half of the check this reports.
		{"an empty value where English is empty too", "export default {\n    IntentionallyBlank: '',\n};\n"},
		// Nothing to compare and nothing to report.
		{"an empty translations object", "export default {};\n"},
		// Shapes carrying no comparable string literal at all.
		{"a non-string value", "export default {\n    Greeting: 42,\n};\n"},
		{"a computed key", "const key = 'Greeting';\nexport default {\n    [key]: 'Hello there',\n};\n"},
		{"a spread", "const other = { Greeting: 'Hola' };\nexport default {\n    ...other,\n};\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := runOnTranslations(t, map[string]string{
				"translations/en.ts": englishTranslations,
				"translations/es.ts": testCase.spanish,
			}, "translations/es.ts")
			rule_testing.ExpectClean(t, result)
		})
	}
}

// Every filename exclusion is a file that would otherwise be compared against itself or against
// nothing. en.ts is the sharpest: it is the basis of the comparison, so every value in it is
// identical to English by definition and the rule would report the entire file.
func TestLocalizationNoUntranslatedValueDeclinesFilesThatAreNotLocaleData(t *testing.T) {
	// Content identical to English, so any file this rule agrees to read will report. That is what
	// makes these fixtures discriminate rather than merely pass.
	const copiedFromEnglish = "export default {\n    Greeting: 'Hello there',\n};\n"

	cases := []struct {
		name         string
		relativePath string
	}{
		{"en.ts itself", "translations/en.ts"},
		{"index.ts wiring", "translations/index.ts"},
		{"locales.ts wiring", "translations/locales.ts"},
		{"a Translations.ts type file", "translations/AccountTranslations.ts"},
		{"a TranslationsType.ts type file", "translations/AccountTranslationsType.ts"},
		{"an Interface.ts type file", "translations/AccountInterface.ts"},
		// Outside a translations directory this is just an object literal that happens to hold
		// English strings, which describes a great deal of ordinary code.
		{"a file outside any translations directory", "source/es.ts"},
		// A whole path segment, not a substring. Matching loosely would start linting whatever a
		// directory named this way happens to hold.
		{"a directory whose name merely contains translations", "translations-archive/es.ts"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			files := map[string]string{
				"translations/en.ts":         englishTranslations,
				"translations-archive/en.ts": englishTranslations,
				"source/en.ts":               englishTranslations,
			}
			files[testCase.relativePath] = copiedFromEnglish
			result := runOnTranslations(t, files, testCase.relativePath)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// An underscore-prefixed translations directory is the other spelling in the tree, and it must be
// read rather than skipped. A locale directory this rule silently declines is a locale nobody is
// checking, which reads exactly like a fully translated one.
func TestLocalizationNoUntranslatedValueReadsUnderscoreTranslationsDirectories(t *testing.T) {
	result := runOnTranslations(t, map[string]string{
		"_translations/en.ts": englishTranslations,
		"_translations/es.ts": "export default {\n    Greeting: 'Hello there',\n};\n",
	}, "_translations/es.ts")
	rule_testing.ExpectFindings(t, result, "identicalToSource")
}

// A locale file whose en.ts is missing is declined rather than reported on.
//
// The alternative is reporting every key as untranslatable because the comparison basis was absent,
// which is a wall of findings that says nothing about the translations.
func TestLocalizationNoUntranslatedValueDeclinesWithoutAnEnglishSibling(t *testing.T) {
	result := runOnTranslations(t, map[string]string{
		"translations/es.ts": "export default {\n    Greeting: 'Hello there',\n};\n",
	}, "translations/es.ts")
	rule_testing.ExpectClean(t, result)
}

// The rule reports and never rewrites: the repair is a translation, which a rule cannot write.
func TestLocalizationNoUntranslatedValueProposesNoFix(t *testing.T) {
	result := runOnTranslations(t, map[string]string{
		"translations/en.ts": englishTranslations,
		"translations/es.ts": "export default {\n    Greeting: 'Hello there',\n};\n",
	}, "translations/es.ts")
	rule_testing.ExpectFindings(t, result, "identicalToSource")

	if len(result.Diagnostics[0].Fixes) != 0 {
		t.Fatalf("expected no fixes, got %d", len(result.Diagnostics[0].Fixes))
	}
	if len(result.Diagnostics[0].Suggestions) != 0 {
		t.Fatalf("expected no suggestions, got %d", len(result.Diagnostics[0].Suggestions))
	}
}

// A file outside a translations directory is declined before any node is visited, and until now
// nothing measured that.
//
// Every other fixture in this file sits under a translations path, so the cheapest and most
// load-bearing guard in the rule was never exercised: making `isTranslationFile` always true left
// the whole suite green. That guard is what keeps this rule free on the 3,400 files that are not
// locale data, and a rule that started walking every file in the tree would have looked exactly like
// this from the outside.
//
// The shape generalizes past this rule. 32 of the rules in this repository return early before
// returning any listener, and each of those guards can silence every fixture written for the
// behavior behind it. A sweep that returns several survivors at once is the tell: individually they
// read as several gaps, and they are usually one guard upstream of all of them.
func TestLocalizationNoUntranslatedValueDeclinesOrdinaryFiles(t *testing.T) {
	// The subject sits outside any translations directory and carries a value identical to the
	// English one, so the only reason it reports nothing is the path guard. Placing it inside
	// `translations/` would make it a locale file by the rule's own definition, which is what the
	// guard is deciding and not something a fixture gets to assume away.
	// An English sibling sits beside the subject, so a bypassed path guard would find one and fire.
	// Without it the run stops at the next guard instead and the clean result would say nothing:
	// measured by making `isTranslationFile` always true and watching this test still pass, which is
	// the dead fixture it exists to rule out.
	result := runOnTranslations(t, map[string]string{
		"translations/en.ts": englishTranslations,
		"app/en.ts":          englishTranslations,
		"app/Probe.ts":       "export default {\n    Greeting: 'Hello there',\n};\n",
	}, "app/Probe.ts")

	rule_testing.ExpectClean(t, result)
}
