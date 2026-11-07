package go_origin_names_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/internal/naming/go_origin_names"
)

// The policy's vocabulary reads into the three kinds of word an error's name is built from: a word with one
// expansion expands, a word with several meanings or advice of its own is left to a hand, and the millisecond
// word, judged by a matcher, is too.
func TestVocabularyOfSortsEveryWord(test *testing.T) {
	test.Parallel()
	words := go_origin_names.VocabularyOf(abbreviation.Policy)
	for word, expansion := range map[string]string{"cfg": "configuration", "err": "error", "src": "source"} {
		if words.Expansions[word] != expansion {
			test.Errorf("%q expands to %q, want %q", word, words.Expansions[word], expansion)
		}
	}
	for _, word := range []string{"pos", "fn", "sb"} {
		if !words.AdviceWords[word] {
			test.Errorf("%q is not an advice word", word)
		}
	}
	if !words.MatcherWords["ms"] {
		test.Error(`"ms" is not a matcher word`)
	}
	for word := range words.Expansions {
		if words.AdviceWords[word] || words.MatcherWords[word] {
			test.Errorf("%q is both an expansion and a word left to a hand", word)
		}
	}
}
