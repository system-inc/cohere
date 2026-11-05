// Package sentence turns an error chain into the sentence a person reads.
//
// A Go error is a lowercase clause, and each caller that wraps it with %w adds its own clause in front,
// so a chain reads "checking the workspace: reading go.work: no such file". That is the shape Go asks for,
// because a capital or a period in the middle of a chain reads wrong. A person reads the chain once, at
// the edge where it is printed, and there it is a sentence: a capital first letter and a closing period
// (ruling 4 on #pp04nv8). This package is that edge, so every place cohere prints an error to a person,
// the front door, the launcher and the Go engine alike, renders it the same way, and no error string
// anywhere carries its own capital or period.
package sentence

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FromError is the sentence a person reads for an error chain.
func FromError(sourceError error) string {
	return Of(sourceError.Error())
}

// Of is a clause as a sentence: its first letter capitalized when its first word is an ordinary word,
// and a period closing it when it is one line ending on an ordinary word.
//
// A literal keeps its spelling at either end, because changing it would misspell it or change what a
// reader copies. At the start that is a flag (`--tsconfig`), a file (`go.work`, `tsconfig.json`), a path,
// an identifier with an underscore, or anything in backticks: an ordinary first word is made only of
// letters, followed by nothing or by the colon, semicolon or comma that ends a clause, such as
// "reading:". At the end it is a path, a file, a quoted or backticked value, or an assignment, any of
// which a period would join: `/repo/go.work.` is a file that does not exist. A clause that ends on one is
// left open; the cure is to reword the clause so it ends on words, not to close it wrong here.
//
// Text of several lines closes nothing: its first line is the sentence, and what follows is a list or
// another tool's output, which is data, and a period after its last line would read as part of it.
// Text that already ends on a period, a question mark or an exclamation mark is left as it is, and empty
// text stays empty, since there is no sentence in it to close.
func Of(clause string) string {
	text := strings.TrimRightFunc(clause, unicode.IsSpace)
	if text == "" {
		return text
	}
	first, size := utf8.DecodeRuneInString(text)
	if unicode.IsLower(first) && isOrdinaryWord(firstWord(text)) {
		text = string(unicode.ToUpper(first)) + text[size:]
	}
	if strings.Contains(text, "\n") || endsClosed(text) || isLiteral(lastWord(text)) {
		return text
	}
	return text + "."
}

// firstWord is the text up to its first space or line break.
func firstWord(text string) string {
	if end := strings.IndexFunc(text, unicode.IsSpace); end >= 0 {
		return text[:end]
	}
	return text
}

// lastWord is the text after its last space.
func lastWord(text string) string {
	return text[strings.LastIndexFunc(text, unicode.IsSpace)+1:]
}

// endsClosed reports whether text already ends a sentence, or ends on a colon, which opens one rather than
// closing it.
func endsClosed(text string) bool {
	last, _ := utf8.DecodeLastRuneInString(text)
	return last == '.' || last == '?' || last == '!' || last == ':'
}

// isLiteral reports whether a word is something a reader copies rather than reads: a path, a file name or
// version with a dot inside it, a quoted or backticked value, or an assignment. A closing parenthesis or a
// comma is not a literal's mark, so "(exit 2)" still closes.
func isLiteral(word string) bool {
	return strings.ContainsAny(word, "/\\`\"'=.")
}

// isOrdinaryWord reports whether a word is letters alone, allowing the punctuation that ends a clause.
func isOrdinaryWord(word string) bool {
	word = strings.TrimRight(word, ":;,")
	if word == "" {
		return false
	}
	for _, character := range word {
		if !unicode.IsLetter(character) {
			return false
		}
	}
	return true
}
