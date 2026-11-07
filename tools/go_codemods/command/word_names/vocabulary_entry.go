package main

// vocabularyEntry is one abbreviated word and the forms it is judged in.
type vocabularyEntry struct {
	Abbreviation string `json:"abbreviation"`
	// Expansions lists every common meaning. A word with none (`acc`, `fn`, `sb`) or several (`pos`: position
	// or positive) is never expanded by this command, which would have to guess: see singleExpansion.
	Expansions []string     `json:"expansions"`
	Advice     string       `json:"advice"`
	Reason     string       `json:"reason"`
	Whole      *wholeForm   `json:"whole"`
	Prefix     *prefixForm  `json:"prefix"`
	Suffix     *suffixForm  `json:"suffix"`
	Segment    *segmentForm `json:"segment"`
}

// singleExpansion is the word's expansion when the vocabulary gives exactly one, and empty otherwise, so a word
// with several meanings is listed for a person to name rather than given its first.
func (entry *vocabularyEntry) singleExpansion() string {
	if len(entry.Expansions) != 1 {
		return ""
	}
	return entry.Expansions[0]
}
