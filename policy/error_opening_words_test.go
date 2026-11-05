package policy

import (
	"strings"
	"testing"
)

// minimalErrorOpeningWords is a file every refusal below starts from, changed in one place, so each refusal is the
// one under test rather than some other.
const minimalErrorOpeningWords = `{"about": "a", "words": [{"word": "the"}, {"word": "couldn't"}]}`

// The minimal file loads and holds exactly its words, and each one-place change that would misread is refused.
func TestErrorOpeningWordsLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	vocabulary, loadError := loadErrorOpeningWords([]byte(minimalErrorOpeningWords))
	if loadError != nil {
		test.Fatalf("the minimal file is refused (%v), so no refusal below would mean anything", loadError)
	}
	if !vocabulary.Holds("the") || !vocabulary.Holds("couldn't") || vocabulary.Holds("The") || vocabulary.Holds("a") {
		test.Fatalf("the minimal file holds %v, expected exactly the and couldn't", vocabulary.held)
	}
	for _, refused := range []struct {
		name        string
		original    string
		replacement string
	}{
		{"an unknown top-level key", `"about": "a"`, `"about": "a", "aliases": []`},
		{"an unknown key on a word", `{"word": "the"}`, `{"word": "the", "note": "n"}`},
		{"an empty list", `[{"word": "the"}, {"word": "couldn't"}]`, `[]`},
		{"a missing list", `, "words": [{"word": "the"}, {"word": "couldn't"}]`, ``},
		{"a duplicate", `{"word": "couldn't"}`, `{"word": "the"}`},
		{"a capitalized word", `"word": "the"`, `"word": "The"`},
		{"an empty word", `"word": "the"`, `"word": ""`},
		{"a word with a space", `"word": "the"`, `"word": "the end"`},
		{"a word with a digit", `"word": "the"`, `"word": "the2"`},
		{"a leading apostrophe", `"word": "couldn't"`, `"word": "'twas"`},
		{"a trailing apostrophe", `"word": "couldn't"`, `"word": "couldn'"`},
		{"two apostrophes in a row", `"word": "couldn't"`, `"word": "couldn''t"`},
	} {
		changed := strings.Replace(minimalErrorOpeningWords, refused.original, refused.replacement, 1)
		if changed == minimalErrorOpeningWords {
			test.Fatalf("%s: the change did not apply, so it would test nothing", refused.name)
		}
		if _, refusedError := loadErrorOpeningWords([]byte(changed)); refusedError == nil {
			test.Errorf("%s is accepted: %s", refused.name, changed)
		}
	}
}

// The embedded file loads, which the package's init already proves by not panicking, and holds a word from each
// group the rule relies on, so a file trimmed by mistake is caught here rather than as a rule gone quiet.
func TestErrorOpeningWordsHoldsEachGroup(test *testing.T) {
	test.Parallel()
	for _, word := range []string{"the", "cannot", "couldn't", "invalid", "because"} {
		if !ErrorOpeningWords.Holds(word) {
			test.Errorf("the embedded vocabulary does not hold %q", word)
		}
	}
}
