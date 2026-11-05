package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// An Adamic `.a` translation file is checked as the `.ts` it is, against its English sibling found by
// its own extension first and then the other (#kwt1htp). A set moving to Adamic file by file holds
// both kinds side by side, so each pairing finds its English. Both siblings present is a guess, so it
// is reported and nothing is compared; neither present is reported as missing, naming en.a first
// (#techtr1).
func TestAnAdamicTranslationFileFindsItsEnglishSibling(test *testing.T) {
	test.Parallel()
	english := "export default { Greeting: 'Hello there' };\n"
	copied := "export default { Greeting: 'Hello there' };\n"

	control := runOnTranslations(test, map[string]string{
		"_translations/en.ts": english,
		"_translations/fr.ts": copied,
	}, "_translations/fr.ts")

	for _, testCase := range []struct {
		name  string
		files map[string]string
	}{
		{"en.a beside fr.a", map[string]string{"_translations/en.a": english, "_translations/fr.a": copied}},
		{"en.ts beside fr.a", map[string]string{"_translations/en.ts": english, "_translations/fr.a": copied}},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectSameFindings(test, control, runOnTranslations(test, testCase.files, "_translations/fr.a"))
		})
	}

	test.Run("en.a beside fr.ts", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectSameFindings(test, control, runOnTranslations(test, map[string]string{
			"_translations/en.a":  english,
			"_translations/fr.ts": copied,
		}, "_translations/fr.ts"))
	})

	test.Run("both en.ts and en.a", func(test *testing.T) {
		test.Parallel()
		result := runOnTranslations(test, map[string]string{
			"_translations/en.ts": english,
			"_translations/en.a":  english,
			"_translations/fr.a":  copied,
		}, "_translations/fr.a")
		rule_testing.ExpectFindings(test, result, "ambiguousEnglishSibling")
	})

	test.Run("neither en.ts nor en.a", func(test *testing.T) {
		test.Parallel()
		result := runOnTranslations(test, map[string]string{
			"_translations/de.ts": english,
			"_translations/fr.a":  copied,
		}, "_translations/fr.a")
		rule_testing.ExpectFindings(test, result, "missingEnglishSibling")
		expectMissingSiblingNames(test, result, "en.a", "en.ts")
	})

	// en.a is English, not a locale to check, as en.ts is.
	test.Run("en.a itself", func(test *testing.T) {
		test.Parallel()
		rule_testing.ExpectClean(test, runOnTranslations(test, map[string]string{
			"_translations/en.ts": english,
			"_translations/en.a":  copied,
		}, "_translations/en.a"))
	})
}
