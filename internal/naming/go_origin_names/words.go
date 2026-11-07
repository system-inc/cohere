package go_origin_names

import (
	"strings"
	"unicode"
)

// CamelWords splits a Go name into its camelCase words: a boundary before an uppercase letter that follows a
// lowercase letter or digit, and before the last uppercase letter of a run that a lowercase letter follows
// (`HTTPServer` is `HTTP` and `Server`).
func CamelWords(name string) []string {
	runes := []rune(name)
	var words []string
	start := 0
	for index := 1; index < len(runes); index++ {
		previous, current := runes[index-1], runes[index]
		lowerFollows := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if unicode.IsUpper(current) &&
			(unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && lowerFollows)) {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	return append(words, string(runes[start:]))
}

// HouseCased writes a name's word as the house writes it: an initialism as any other word (`CSS` after the
// first word is `Css`), and the first word lowered whole.
func HouseCased(word string, index int) string {
	if index == 0 {
		return strings.ToLower(word[:1]) + word[1:]
	}
	if len(word) > 1 && strings.ToUpper(word) == word && strings.ToLower(word) != word {
		return word[:1] + strings.ToLower(word[1:])
	}
	return word
}

// withoutTrailingWords drops name's trailing camel words that are in trailing, keeping at least one word.
func withoutTrailingWords(name string, trailing map[string]bool) string {
	words := CamelWords(name)
	for len(words) > 1 && trailing[strings.ToUpper(words[len(words)-1][:1])+words[len(words)-1][1:]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, "")
}
