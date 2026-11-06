package policy

// ErrorOpeningVocabulary is ErrorOpeningWords.json, read and checked.
type ErrorOpeningVocabulary struct {
	About string             `json:"about"`
	Words []ErrorOpeningWord `json:"words"`

	// held is Words as a set, built when the file is read, so a lookup does not walk the list.
	held map[string]bool
}

// Holds is whether word, lowercase, is in the vocabulary.
func (vocabulary ErrorOpeningVocabulary) Holds(word string) bool {
	return vocabulary.held[word]
}
