package abbreviation_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// JavaScript's `var` is a keyword, not short for "variable", so a name holding one of policy/KeywordTerms.json's
// spellings for it reads clean, and every other `Var` segment is still the abbreviation (#hvyvfr5). The same names
// against a vocabulary loaded with no keyword terms are the positive control: each clean name is reported there, so
// it is the keyword term that silences it, never a rule that judged nothing.
func TestFindReadsAKeywordTermAsAWord(test *testing.T) {
	test.Parallel()
	withoutKeywordTerms, loadError := abbreviation.Load(policy.AbbreviationsFile(), nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	clean := []string{
		"isVarDeclaration", "VarKeywordFix", "VarDeclarations", "TopLevelVarScopedDeclarations",
		"collectVarDeclaredNames", "VarKeyword", "hoistVarDeclarationsToTop", "VarScoped2",
	}
	for _, name := range clean {
		if finding, found := abbreviation.Policy.Find(name); found {
			test.Errorf("%q is reported, and it names the keyword: %+v", name, finding.Values)
		}
		if finding, found := withoutKeywordTerms.Find(name); !found || finding.Values["word"] != "Var" {
			test.Errorf("%q is not reported as Var with no keyword terms (%t, %+v), so its silence proves nothing",
				name, found, finding.Values)
		}
	}
	reported := []struct{ name, word string }{
		// A bare `Var` is no keyword term: it names no next word, so only its author knows which it means.
		{"NoVar", "Var"},
		{"themeVar", "Var"},
		{"themeVarName", "Var"},
		// A spelling ends at a word boundary, so a longer word that opens with it is not the spelling.
		{"VarScopedness", "Var"},
		{"isVarKeywordish", "Var"},
		// A keyword term speaks for its own segment alone: a bare `Var` beside it, or another word, still reports.
		{"VarDeclarationForThemeVar", "Var"},
		{"themeVarBeforeVarKeyword", "Var"},
		{"VarKeywordCmd", "Cmd"},
		{"VarDeclarationSrcPath", "Src"},
		// `vars` precedes `var`, and `Vars` is no spelling of the keyword.
		{"VarKeywordVars", "Vars"},
	}
	for _, testCase := range reported {
		finding, found := abbreviation.Policy.Find(testCase.name)
		if !found || finding.Form != abbreviation.FormSegment || finding.Values["word"] != testCase.word {
			test.Errorf("%q: got %t %q %+v, want the %q segment", testCase.name, found, finding.Form, finding.Values,
				testCase.word)
		}
	}
}

// A keyword term silences one entry's segment form, and only an advice-style one: a rename rewrites the word's first
// segment, which may be the keyword's own, and the keyword has no full word to rename it to. A term with no such
// entry would silence nothing, or a rename, so the loader refuses both, and accepts the entry beside them.
func TestLoadRefusesAKeywordTermWithNoAdvisingSegment(test *testing.T) {
	test.Parallel()
	keywordTerms := []policy.KeywordTerm{{Term: "var", Spellings: []string{"VarKeyword"}}}
	for _, refused := range []struct{ name, file string }{
		{"no entry for the term", `{"abbreviations": [
			{"abbreviation": "val", "expansions": ["value"], "segment": {}}]}`},
		{"an entry with no segment form", `{"abbreviations": [
			{"abbreviation": "var", "advice": "Say.", "whole": {"style": "advice"}}]}`},
		{"a segment that renames", `{"abbreviations": [
			{"abbreviation": "var", "expansions": ["variable"], "segment": {}}]}`},
	} {
		if _, refusedError := abbreviation.Load([]byte(refused.file), keywordTerms); refusedError == nil {
			test.Errorf("%s: the loader accepted it", refused.name)
		}
	}
	accepted, acceptedError := abbreviation.Load([]byte(`{"abbreviations": [
		{"abbreviation": "var", "advice": "Say.", "segment": {"style": "advice"}}]}`), keywordTerms)
	if acceptedError != nil {
		test.Fatalf("an advice-style segment with its keyword term is refused: %v", acceptedError)
	}
	if _, found := accepted.Find("isVarKeyword"); found {
		test.Error("the accepted keyword term silences nothing")
	}
}
