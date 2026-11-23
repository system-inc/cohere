// Package words imports nothing of ours, so its check runs beside the diamond's.
package words

import (
	"sort"
	"strings"
	"unicode"
)

// Counted is a word and how often it appears.
type Counted struct {
	Word  string
	Times int
}

// Count counts text's words, most frequent first.
func Count(text string) []Counted {
	times := map[string]int{}
	for _, word := range strings.FieldsFunc(text, func(letter rune) bool { return !unicode.IsLetter(letter) }) {
		times[strings.ToLower(word)]++
	}
	counted := make([]Counted, 0, len(times))
	for word, each := range times {
		counted = append(counted, Counted{Word: word, Times: each})
	}
	sort.Slice(counted, func(a int, b int) bool { return counted[a].Times > counted[b].Times })
	return counted
}
