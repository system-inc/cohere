package policy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
)

// ErrorOpeningWords.json is the closed vocabulary of ordinary English words the Go engine's
// consistency-require-error-clause flags when an error's text opens on one capitalized. It is data rather than
// code so a word joins or leaves the list as a decision about the house, in one place.
//
//go:embed ErrorOpeningWords.json
var errorOpeningWordsFile []byte

// ErrorOpeningWord is one entry of the vocabulary: a word, lowercase, as it would read mid-sentence.
type ErrorOpeningWord struct {
	Word string `json:"word"`
}

// ErrorOpeningWords is the embedded file, read once. An invalid file panics at startup, since a vocabulary read
// wrongly would let the rule flag a proper name or miss every ordinary word while its tests reported it clean.
var ErrorOpeningWords = mustLoadErrorOpeningWords(errorOpeningWordsFile)

func mustLoadErrorOpeningWords(data []byte) ErrorOpeningVocabulary {
	vocabulary, loadError := loadErrorOpeningWords(data)
	if loadError != nil {
		// Invariant: the file is embedded and its loader is tested, so a refusal here is a broken build of
		// policy, and running with no vocabulary would be a rule that silently flags nothing.
		panic(fmt.Sprintf("ErrorOpeningWords.json: %v", loadError))
	}
	return vocabulary
}

// loadErrorOpeningWords reads the file, refusing an unknown key, an empty list, a duplicate, and a word that is not
// lowercase letters with at most an apostrophe between them, since each would read as something other than meant.
func loadErrorOpeningWords(data []byte) (ErrorOpeningVocabulary, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var vocabulary ErrorOpeningVocabulary
	if decodeError := decoder.Decode(&vocabulary); decodeError != nil {
		return ErrorOpeningVocabulary{}, decodeError
	}
	if len(vocabulary.Words) == 0 {
		return ErrorOpeningVocabulary{}, fmt.Errorf("words is empty, so the rule would flag nothing")
	}
	vocabulary.held = make(map[string]bool, len(vocabulary.Words))
	for _, entry := range vocabulary.Words {
		if !isLowercaseWord(entry.Word) {
			return ErrorOpeningVocabulary{}, fmt.Errorf(
				"%q is not lowercase letters, with at most an apostrophe between them",
				entry.Word,
			)
		}
		if vocabulary.held[entry.Word] {
			return ErrorOpeningVocabulary{}, fmt.Errorf("%q appears twice", entry.Word)
		}
		vocabulary.held[entry.Word] = true
	}
	return vocabulary, nil
}

// isLowercaseWord is whether word is lowercase ASCII letters, with an apostrophe allowed only between two letters,
// the shape of a contraction such as couldn't. The rule lowercases the word it reads before the lookup, so a capital
// here could never match.
func isLowercaseWord(word string) bool {
	if word == "" {
		return false
	}
	for index := 0; index < len(word); index++ {
		character := word[index]
		if character >= 'a' && character <= 'z' {
			continue
		}
		betweenLetters := index > 0 && index < len(word)-1 && word[index-1] != '\'' && word[index+1] != '\''
		if character != '\'' || !betweenLetters {
			return false
		}
	}
	return true
}
