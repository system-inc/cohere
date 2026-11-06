package shouting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/system-inc/cohere/policy"
)

// The shouting vocabulary, read from cohere's policy/Shouting.json rather than written here as code.
//
// It is data because three engines read it. The TypeScript rule and the Go engine's rule read it through this
// package, and the Swift engine compiles in the same bytes (policy.SwiftFiles), so an acronym added to the file
// quiets all three at once, as the abbreviation vocabulary does (#2cqemcd). Each group carries its reason with it,
// so the file can be pruned by someone who was not there when an entry was earned.
//
// What stays in Go is the judgment: the shape tests in isShoutedToken and the masks that hide code. The file is
// decoded with the standard library alone, as the rest of this package is, since the Go engine links it and must
// not pull in the TypeScript checker.

// vocabularyFile is the file's shape. Unknown keys are refused when it is read, so a misspelled group is a startup
// failure rather than a list that silently allows nothing.
type vocabularyFile struct {
	About                 string           `json:"about"`
	Allowed               []allowedGroup   `json:"allowed"`
	ShoutedTwoLetterWords twoLetterWordSet `json:"shoutedTwoLetterWords"`
}

// allowedGroup is one group of words that are never shouting, with the reason the group earns its place.
type allowedGroup struct {
	Reason string   `json:"reason"`
	Tokens []string `json:"tokens"`
}

// twoLetterWordSet is the two-letter words that are shouting in capitals.
type twoLetterWordSet struct {
	Reason string   `json:"reason"`
	Words  []string `json:"words"`
}

// allowedUppercaseTokens are every allowed group's words, which are never shouting whatever their shape:
// acronyms, file names and ISO 4217 currency codes. shoutedTwoLetterWords are the short words the vowel test
// cannot judge. Both are policy's file, read once. An invalid file panics at startup, which is the loud failure:
// a vocabulary read wrongly would make the rules quietly judge differently than they appear to.
var allowedUppercaseTokens, shoutedTwoLetterWords = mustLoadVocabulary(policy.ShoutingFile())

// vocabularyWord is the shape every listed word has: what the token pattern can match, so a word the rules could
// never see is refused instead of listed.
var vocabularyWord = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func mustLoadVocabulary(data []byte) (map[string]bool, map[string]bool) {
	allowed, twoLetterWords, err := loadVocabulary(data)
	if err != nil {
		panic(fmt.Sprintf("policy/Shouting.json: %v", err))
	}
	return allowed, twoLetterWords
}

func loadVocabulary(data []byte) (map[string]bool, map[string]bool, error) {
	var file vocabularyFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, nil, errors.New("there is more after the JSON value")
	}
	if len(file.Allowed) == 0 {
		return nil, nil, errors.New("no allowed group, so every acronym with a vowel would read as shouting")
	}
	allowed := map[string]bool{}
	for index, group := range file.Allowed {
		if group.Reason == "" || len(group.Tokens) == 0 {
			return nil, nil, fmt.Errorf("allowed group %d needs a reason and at least one token", index)
		}
		for _, token := range group.Tokens {
			if !vocabularyWord.MatchString(token) {
				return nil, nil, fmt.Errorf("allowed group %d lists %q, which is not an uppercase word", index, token)
			}
			allowed[token] = true
		}
	}
	if file.ShoutedTwoLetterWords.Reason == "" || len(file.ShoutedTwoLetterWords.Words) == 0 {
		return nil, nil, errors.New("shoutedTwoLetterWords needs a reason and at least one word")
	}
	twoLetterWords := map[string]bool{}
	for _, word := range file.ShoutedTwoLetterWords.Words {
		if len(word) != 2 || !vocabularyWord.MatchString(word) {
			return nil, nil, fmt.Errorf("shoutedTwoLetterWords lists %q, which is not a two-letter uppercase word", word)
		}
		twoLetterWords[word] = true
	}
	return allowed, twoLetterWords, nil
}
