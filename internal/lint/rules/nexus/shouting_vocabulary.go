package nexus

import (
	"fmt"
	"regexp"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/policy"
)

// The shouting vocabulary, read from cohere's policy/Shouting.json rather than written here as code.
//
// It is data because two engines read it. The Swift engine's shouting rule must call the same words acronyms
// this rule does, so the lists live in one file under policy/, which both engines compile in, as the abbreviation
// vocabulary does (#2cqemcd). Each group carries its reason with it, so the file can be pruned by someone who was
// not there when an entry was earned.
//
// What stays in Go is the judgment: the shape tests in isShoutedToken and the masks that hide code.

// shoutingVocabularyFile is the file's shape. Unknown keys are refused when it is read, so a misspelled group is a
// startup failure rather than a list that silently allows nothing.
type shoutingVocabularyFile struct {
	About                 string                   `json:"about"`
	Allowed               []shoutingAllowedGroup   `json:"allowed"`
	ShoutedTwoLetterWords shoutingTwoLetterWordSet `json:"shoutedTwoLetterWords"`
}

// shoutingAllowedGroup is one group of words that are never shouting, with the reason the group earns its place.
type shoutingAllowedGroup struct {
	Reason string   `json:"reason"`
	Tokens []string `json:"tokens"`
}

// shoutingTwoLetterWordSet is the two-letter words that are shouting in capitals.
type shoutingTwoLetterWordSet struct {
	Reason string   `json:"reason"`
	Words  []string `json:"words"`
}

// allowedUppercaseTokens are every allowed group's words, which are never shouting whatever their shape:
// acronyms, file names and ISO 4217 currency codes. shoutedTwoLetterWords are the short words the vowel test
// cannot judge. Both are policy's file, read once. An invalid file panics at startup, which is the loud failure:
// a vocabulary read wrongly would make the rule quietly judge differently than it appears to.
var allowedUppercaseTokens, shoutedTwoLetterWords = mustLoadShoutingVocabulary(policy.ShoutingFile())

// shoutingVocabularyWord is the shape every listed word has: what the token pattern can match, so a word the rule
// could never see is refused instead of listed.
var shoutingVocabularyWord = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func mustLoadShoutingVocabulary(data []byte) (map[string]bool, map[string]bool) {
	allowed, twoLetterWords, err := loadShoutingVocabulary(data)
	if err != nil {
		panic(fmt.Sprintf("policy/Shouting.json: %v", err))
	}
	return allowed, twoLetterWords
}

func loadShoutingVocabulary(data []byte) (map[string]bool, map[string]bool, error) {
	var file shoutingVocabularyFile
	if err := rule.UnmarshalOptions(data, &file); err != nil {
		return nil, nil, err
	}
	if len(file.Allowed) == 0 {
		return nil, nil, fmt.Errorf("no allowed group, so every acronym with a vowel would read as shouting")
	}
	allowed := map[string]bool{}
	for index, group := range file.Allowed {
		if group.Reason == "" || len(group.Tokens) == 0 {
			return nil, nil, fmt.Errorf("allowed group %d needs a reason and at least one token", index)
		}
		for _, token := range group.Tokens {
			if !shoutingVocabularyWord.MatchString(token) {
				return nil, nil, fmt.Errorf("allowed group %d lists %q, which is not an uppercase word", index, token)
			}
			allowed[token] = true
		}
	}
	if file.ShoutedTwoLetterWords.Reason == "" || len(file.ShoutedTwoLetterWords.Words) == 0 {
		return nil, nil, fmt.Errorf("shoutedTwoLetterWords needs a reason and at least one word")
	}
	twoLetterWords := map[string]bool{}
	for _, word := range file.ShoutedTwoLetterWords.Words {
		if len(word) != 2 || !shoutingVocabularyWord.MatchString(word) {
			return nil, nil, fmt.Errorf("shoutedTwoLetterWords lists %q, which is not a two-letter uppercase word", word)
		}
		twoLetterWords[word] = true
	}
	return allowed, twoLetterWords, nil
}
