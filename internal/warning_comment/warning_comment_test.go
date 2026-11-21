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
			// The word alone, after a comment without one, and before one, so a gate that reads any one comment of
			// the group, the first or the last, turns away a group it should keep.
			for _, group := range []struct {
				texts []string
				at    int
			}{
				{texts: []string{text}, at: 0},
				{texts: []string{"// nothing here", text}, at: 1},
				{texts: []string{text, "//", "// nothing here"}, at: 0},
			} {
				words := WordsIn(group.texts)
				if len(words) != 1 {
					test.Errorf("WordsIn(%q) = %v, want one word", group.texts, words)
					continue
				}
				got := group.texts[words[0].Comment][words[0].Start:words[0].End]
				if words[0].Comment != group.at || got != word {
					test.Errorf("WordsIn(%q) found %q in comment %d, want %q in comment %d",
						group.texts, got, words[0].Comment, word, group.at)
				}
			}
		}
	}
}

// A group with none of the words is turned away before its bodies are joined, so it allocates nothing, and nearly
// every group an engine hands over is that. The group has capitals in a row, the words in lowercase, and a word cut
// in two across a pair of comments, which the joined text holds only with a newline inside it, so it is no word
// either way.
//
// Not parallel: testing.AllocsPerRun reads the process's allocation counters, which a test running beside it would
// add to.
func TestWordsInAllocatesNothingForAGroupWithoutTheWords(test *testing.T) {
	texts := []string{"// HTTP and JSON, a todo list and a fixme branch, then FIX", "// ME and TO", "/* DO */"}
	var words []Word
	allocations := testing.AllocsPerRun(10, func() { words = WordsIn(texts) })
	if len(words) != 0 {
		test.Errorf("WordsIn(%q) = %v, want none", texts, words)
	}
	if allocations != 0 {
		test.Errorf("WordsIn allocated %.0f times over a group with none of the words: it joined the group before "+
			"asking whether any comment holds one", allocations)
	}
}

// HoldsWarningWord says yes wherever the regexp would match and no wherever it would not, so it turns away exactly the
// texts the regexp would and no other: lowercase and mixed case, a word cut short, and an empty text are turned away,
// and a word inside a longer one passes on to the whole-word test.
func TestHoldsWarningWordAgreesWithTheRegexp(test *testing.T) {
	test.Parallel()

	for _, text := range []string{
		"", "todo", "Todo", "fixme", "Fixme", "xxx", "XxX", "TOD", "FIXM", "XX", "TO DO", "FIX ME", "X X X",
		"TODO", "FIXME", "XXX", "TODOS", "TODO_LIST", "XXXX", "0xXXX", "ÜTODO", "a\nFIXME", "ends in XXX",
	} {
		want := warningWord.MatchString(text)
		if got := HoldsWarningWord(text); got != want {
			test.Errorf("HoldsWarningWord(%q) = %t, want %t as the regexp says", text, got, want)
		}
	}
}
