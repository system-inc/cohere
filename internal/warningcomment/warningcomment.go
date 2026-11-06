// Package warningcomment is what counts as a task left for later in a comment, in one place, for every engine that
// judges comments (#4b337jk, textrules-decisions.md "Warning comments").
//
// The TypeScript rule nexus/consistency-no-warning-comment and the Go engine's cohere-go/consistency-no-warning-comment
// both read it, so the words, what makes one a whole word, and which text is code are decided once. Each engine
// hands it a group of comments, read as one text, and applies only its own language's facts first: which comments
// form a group, and which are a tool's directive rather than prose. It lives in its own package, importing nothing
// heavier than internal/shouting, because the nexus rules package links the TypeScript checker and the Go engine
// must not.
//
// The words are `TODO`, `FIXME` and `XXX`, matched as written, in capitals, since a lowercase todo is ordinary
// English, and only as whole words, so a sentence about `TODOS` or a name like `TODO_LIST` is prose. Anything in
// backticks is a mention of the word rather than a task, as this package's own comments mention them, and so is a
// code block, which is judged by internal/shouting's indented block mask, so the shouting rule and this one never
// disagree about one.
package warningcomment

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/shouting"
)

var (
	// inlineBackticks is a span of code in a comment, which may wrap onto the next comment of the group. A fence is
	// three backticks opening and three closing, so a fence that holds no backtick of its own falls inside the pair
	// its third opening backtick and first closing one make.
	inlineBackticks = regexp.MustCompile("`[^`]*`")

	// warningWord finds the candidates. Whether one is a whole word is isWholeWord's to say, since Go's \b knows only
	// ASCII and would read ÜTODO as holding the word.
	warningWord = regexp.MustCompile(`TODO|FIXME|XXX`)
)

// Word is one warning word in a comment group: which of the group's comments holds it, and where, as byte offsets
// into that comment's text, so texts[Comment][Start:End] is the word.
type Word struct {
	Comment int
	Start   int
	End     int
}

// WordsIn returns every warning word in a group of comments outside backticks and code blocks, in source order.
// texts are the group's comments as their source spells them, delimiters included, in source order.
//
// The comments' bodies are joined as one text before they are masked, so a code block opens only after a blank
// comment line, and a backtick pair wrapped across two `//` lines still hides the word inside it. A line comment's
// body is its text after the slashes and one space, which is the same in Go and TypeScript, and a block comment's
// is its whole text, since the mask knows its gutter. Every body is a suffix of its comment's text, so an offset in
// the body maps back by the length cut.
func WordsIn(texts []string) []Word {
	type commentSpan struct {
		start int
		end   int
		cut   int
	}
	var joined strings.Builder
	spans := make([]commentSpan, 0, len(texts))
	for index, text := range texts {
		if index > 0 {
			joined.WriteByte('\n')
		}
		body := shouting.GoCommentBody(text)
		start := joined.Len()
		joined.WriteString(body)
		spans = append(spans, commentSpan{start: start, end: joined.Len(), cut: len(text) - len(body)})
	}
	text := joined.String()
	// Most comments hold none of the words, so they skip the masking.
	if !warningWord.MatchString(text) {
		return nil
	}
	// Code blocks are masked before backticks, in the order the shouting rule masks them, so a backtick inside a code
	// block opens no span over the prose after it. Each byte of a span becomes one space and each newline stays, so
	// every offset still points into the source.
	masked := inlineBackticks.ReplaceAllStringFunc(shouting.MaskIndentedCodeBlocks(text), func(match string) string {
		blanked := []byte(match)
		for index, character := range blanked {
			if character != '\n' {
				blanked[index] = ' '
			}
		}
		return string(blanked)
	})
	var words []Word
	for index, span := range spans {
		for _, location := range warningWord.FindAllStringIndex(masked[span.start:span.end], -1) {
			start, end := span.cut+location[0], span.cut+location[1]
			if !isWholeWord(texts[index], start, end) {
				continue
			}
			words = append(words, Word{Comment: index, Start: start, End: end})
		}
	}
	return words
}

// isWholeWord is whether text[start:end] stands alone: no letter, digit or underscore of any script touches it on
// either side, so `TODOS`, `TODO_LIST`, `XXXX`, `0xXXX` and `ÜTODO` are other words.
func isWholeWord(text string, start int, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	return !isWordCharacter(before) && !isWordCharacter(after)
}

// isWordCharacter is a rune that continues a word. An empty side decodes to utf8.RuneError, which is none.
func isWordCharacter(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}
