package warning_comment

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

// The cheap test before the masks must never turn a comment away that holds a word. Each word is found at the start,
// in the middle and at the end of a line comment and of a block comment, alone in its group and as the last comment
// of a group whose first comment holds none, so a test that reads only the start, only one comment or only some of
// the words fails here.
func TestWordsInFindsEachWordWhereverItStands(test *testing.T) {
	test.Parallel()

	for _, word := range []string{"TODO", "FIXME", "XXX"} {
		for _, text := range []string{
			"// " + word + " at the start",
			"// in the " + word + " middle",
			"// at the end " + word,
			"/* " + word + " at the start */",
			"/* in the " + word + " middle */",
			"/* at the end\n   " + word + " */",
		} {
			for _, texts := range [][]string{{text}, {"// nothing here", text}} {
				words := WordsIn(texts)
				if len(words) != 1 {
					test.Errorf("WordsIn(%q) = %v, want one word", texts, words)
					continue
				}
				got := texts[words[0].Comment][words[0].Start:words[0].End]
				if words[0].Comment != len(texts)-1 || got != word {
					test.Errorf("WordsIn(%q) found %q in comment %d, want %q in comment %d",
						texts, got, words[0].Comment, word, len(texts)-1)
				}
			}
		}
	}
}

// holdsWarningWord says yes wherever the regexp would match and no wherever it would not, so it turns away exactly the
// texts the regexp would and no other: lowercase and mixed case, a word cut short, and an empty text are turned away,
// and a word inside a longer one passes on to the whole-word test.
func TestHoldsWarningWordAgreesWithTheRegexp(test *testing.T) {
	test.Parallel()

	for _, text := range []string{
		"", "todo", "Todo", "fixme", "Fixme", "xxx", "XxX", "TOD", "FIXM", "XX", "TO DO", "FIX ME", "X X X",
		"TODO", "FIXME", "XXX", "TODOS", "TODO_LIST", "XXXX", "0xXXX", "ÜTODO", "a\nFIXME", "ends in XXX",
	} {
		want := warningWord.MatchString(text)
		if got := holdsWarningWord(text); got != want {
			test.Errorf("holdsWarningWord(%q) = %t, want %t as the regexp says", text, got, want)
		}
	}
}
