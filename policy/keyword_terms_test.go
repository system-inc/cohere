package policy

import (
	"slices"
	"strings"
	"testing"
)

// minimalKeywordTerms is a file every refusal below starts from, changed in one place, so each refusal is the one
// under test rather than some other.
const minimalKeywordTerms = `{"about": "a", "keywordTerms": [` +
	`{"term": "var", "language": "JavaScript", "spellings": ["VarKeyword", "VarScoped"], "reason": "r", ` +
	`"citation": "https://example.com/var"}, ` +
	`{"term": "let", "language": "JavaScript", "spellings": ["LetKeyword"], "reason": "r", ` +
	`"citation": "https://example.com/let"}]}`

// The minimal file loads, and each one-place change that would misread, or would admit an entry the bar refuses, is
// refused: above all a spelling that is the keyword alone, which would exempt every use of the letters.
func TestKeywordTermsLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	vocabulary, loadError := loadKeywordTerms([]byte(minimalKeywordTerms))
	if loadError != nil {
		test.Fatalf("the minimal file is refused (%v), so no refusal below would mean anything", loadError)
	}
	if len(vocabulary.KeywordTerms) != 2 || !slices.Equal(vocabulary.KeywordTerms[0].Spellings,
		[]string{"VarKeyword", "VarScoped"}) {
		test.Fatalf("the minimal file reads as %+v", vocabulary.KeywordTerms)
	}
	for _, refused := range []struct {
		name        string
		original    string
		replacement string
	}{
		{"an unknown top-level key", `"about": "a"`, `"about": "a", "aliases": []`},
		{"an unknown key on an entry", `"language": "JavaScript", "spellings": ["VarKeyword"`,
			`"language": "JavaScript", "note": "n", "spellings": ["VarKeyword"`},
		{"an empty about", `"about": "a"`, `"about": " "`},
		{"a term listed twice", `"term": "let"`, `"term": "var"`},
		{"a capitalized term", `"term": "var"`, `"term": "Var"`},
		{"a term with a digit", `"term": "var"`, `"term": "var2"`},
		{"an empty term", `"term": "var"`, `"term": ""`},
		{"no language", `"language": "JavaScript", "spellings": ["VarKeyword"`, `"spellings": ["VarKeyword"`},
		{"no spellings", `"spellings": ["VarKeyword", "VarScoped"]`, `"spellings": []`},
		{"a spelling that is the keyword alone", `"VarScoped"`, `"Var"`},
		{"a spelling of another word", `"VarScoped"`, `"LetScoped"`},
		{"a spelling not capitalized", `"VarScoped"`, `"varScoped"`},
		{"a spelling that runs on in lowercase", `"VarScoped"`, `"Varscoped"`},
		{"a spelling with an underscore", `"VarScoped"`, `"Var_Scoped"`},
		{"a spelling listed twice", `"VarScoped"`, `"VarKeyword"`},
		{"no reason", `"reason": "r", "citation": "https://example.com/var"`,
			`"reason": " ", "citation": "https://example.com/var"`},
		{"no citation", `, "citation": "https://example.com/var"`, ``},
		{"a citation that is not an address", `"https://example.com/var"`, `"the specification"`},
		{"a citation over plain http", `"https://example.com/var"`, `"http://example.com/var"`},
	} {
		changed := strings.Replace(minimalKeywordTerms, refused.original, refused.replacement, 1)
		if changed == minimalKeywordTerms {
			test.Fatalf("%s: the change did not apply, so it would test nothing", refused.name)
		}
		if _, refusedError := loadKeywordTerms([]byte(changed)); refusedError == nil {
			test.Errorf("%s is accepted: %s", refused.name, changed)
		}
	}
}

// The embedded file loads, which the package's init already proves by not panicking, and holds `var` as JavaScript's
// keyword, the term the list was opened for, so a file trimmed by mistake is caught here rather than as a finding come
// back.
func TestKeywordTermsHoldTheJavaScriptKeyword(test *testing.T) {
	test.Parallel()
	index := slices.IndexFunc(KeywordTerms.KeywordTerms, func(entry KeywordTerm) bool { return entry.Term == "var" })
	if index < 0 || KeywordTerms.KeywordTerms[index].Language != "JavaScript" ||
		!slices.Contains(KeywordTerms.KeywordTerms[index].Spellings, "VarDeclaration") {
		test.Errorf("the embedded vocabulary has no JavaScript var spelled VarDeclaration: %+v", KeywordTerms)
	}
}
