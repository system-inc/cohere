package warningcomment

import (
	"testing"
)

// Each word comes back naming the comment that holds it and its offsets in that comment's own text, delimiters
// included, so an engine places it by adding the comment's start. The group is read as one text, so the pair
// wrapped across the second and third comments masks the word inside it, and a word in the third, outside the
// pair, is still reported there.
func TestWordsInPlacesEachWordInItsOwnComment(test *testing.T) {
	test.Parallel()

	texts := []string{
		"// nothing to do here",
		"//   TODO after the slashes, and a pair `wraps",
		"// XXX across` the line, then FIXME",
		"/* a block, then TODO */",
	}
	type placed struct {
		comment int
		word    string
		start   int
	}
	wants := []placed{{1, "TODO", 5}, {2, "FIXME", 30}, {3, "TODO", 17}}
	words := WordsIn(texts)
	if len(words) != len(wants) {
		test.Fatalf("got %d words %v, want %d", len(words), words, len(wants))
	}
	for index, want := range wants {
		word := words[index]
		got := placed{word.Comment, texts[word.Comment][word.Start:word.End], word.Start}
		if got != want {
			test.Errorf("word %d is %+v, want %+v", index, got, want)
		}
	}
}

// A group with none of the words, and an empty group, return nothing, so an engine with no comments to hand over
// reports nothing rather than panicking on an empty join.
func TestWordsInFindsNothingInAGroupWithoutTheWords(test *testing.T) {
	test.Parallel()

	for _, texts := range [][]string{nil, {"// todo, Fixme and TODOS are prose"}} {
		if words := WordsIn(texts); len(words) != 0 {
			test.Errorf("WordsIn(%q) = %v, want none", texts, words)
		}
	}
}
