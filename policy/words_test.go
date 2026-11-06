package policy

import (
	"strings"
	"testing"
)

// minimalWords is a file every refusal below starts from, changed in one place, so each refusal is the one under
// test rather than some other.
const minimalWords = "# a header line\n# and a second\nalpha\nbeta\ngamma\n"

// The minimal file loads, skipping its header, and each one-place change that would misread it is refused.
func TestWordsLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	words, loadError := loadWords([]byte(minimalWords))
	if loadError != nil {
		test.Fatalf("the minimal file is refused (%v), so no refusal below would mean anything", loadError)
	}
	if words.Count() != 3 {
		test.Errorf("the minimal file holds %d words, want 3", words.Count())
	}
	for _, read := range []struct {
		word string
		want bool
	}{
		{"alpha", true},
		{"beta", true},
		{"gamma", true},
		{"Alpha", false},
		{"alph", false},
		{"delta", false},
		{"# a header line", false},
		{"", false},
	} {
		if words.Has(read.word) != read.want {
			test.Errorf("Has(%q) is %t, want %t", read.word, !read.want, read.want)
		}
	}
	for _, refused := range []struct {
		name        string
		original    string
		replacement string
	}{
		{"a capital", "beta", "Beta"},
		{"a digit", "beta", "beta2"},
		{"an apostrophe", "beta", "be'ta"},
		{"a space", "beta", "be ta"},
		{"a letter outside ASCII", "beta", "béta"},
		{"an empty line", "beta\n", "beta\n\n"},
		{"a carriage return", "beta\n", "beta\r\n"},
		{"a duplicate", "beta", "alpha"},
		{"a word out of order", "beta\ngamma", "gamma\nbeta"},
		{"a comment after the first word", "beta", "# beta"},
		{"no final newline", "gamma\n", "gamma"},
		{"no words", "alpha\nbeta\ngamma\n", ""},
	} {
		changed := strings.Replace(minimalWords, refused.original, refused.replacement, 1)
		if changed == minimalWords {
			test.Fatalf("%s: the replacement changed nothing, so the case tests nothing", refused.name)
		}
		if _, refusedError := loadWords([]byte(changed)); refusedError == nil {
			test.Errorf("%s is read, and the loader refuses it", refused.name)
		}
	}
}

// The embedded file loads, holds the two lists whole as their generator measured them and the house's supplement,
// and reads the words the separated-folder rule leans on, so a regeneration that lost a list or a word fails here.
func TestKnownWordsReadsTheEmbeddedFile(test *testing.T) {
	test.Parallel()
	words := KnownWords()
	// 151,753 from en_US and 3,820 from software-terms, less the words both hold.
	if words.Count() < 150_000 {
		test.Errorf("Words.txt holds %d words, fewer than en_US alone", words.Count())
	}
	// cgo is HouseWords.txt's, which the generator merges in.
	for _, word := range []string{"format", "options", "allowed", "helpers", "workspace", "javascript", "cgo"} {
		if !words.Has(word) {
			test.Errorf("Words.txt lacks %q", word)
		}
	}
	if words.Has("formatoptions") {
		test.Error("Words.txt holds formatoptions, two words run together")
	}
}
