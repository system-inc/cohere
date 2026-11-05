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
func FromError(err error) string {
	return Of(err.Error())
}

// Of is a clause as a sentence: its first letter capitalized when its first word is an ordinary word,
// and a period closing it unless it already ends on one, a question mark or an exclamation mark.
//
// A first word that is a literal keeps its case, because capitalizing it would misspell it: a flag
// (`--tsconfig`), a file (`go.work`, `tsconfig.json`), a path, an identifier with an underscore, or
// anything in backticks. An ordinary word is one made only of letters, followed by nothing or by the
// colon, semicolon or comma that ends a clause, such as "reading:". Empty text stays empty, since there
// is no sentence in it to close.
func Of(clause string) string {
	text := strings.TrimRightFunc(clause, unicode.IsSpace)
	if text == "" {
		return text
	}
	first, size := utf8.DecodeRuneInString(text)
	if unicode.IsLower(first) && isOrdinaryWord(firstWord(text)) {
		text = string(unicode.ToUpper(first)) + text[size:]
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	if last != '.' && last != '?' && last != '!' {
		text += "."
	}
	return text
}

// firstWord is the text up to its first space or line break.
func firstWord(text string) string {
	if end := strings.IndexFunc(text, unicode.IsSpace); end >= 0 {
		return text[:end]
	}
	return text
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
