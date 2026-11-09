package policy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// KeywordTerms.json is the vocabulary of a language's keywords whose letters Abbreviations.json also lists as an
// abbreviation, JavaScript's `var` above all, with the exact spellings in which the capitalized keyword means the
// keyword (#hvyvfr5). internal/naming/abbreviation reads it, so the TypeScript and Go engines share the one reading,
// and the Swift engine compiles in the same bytes (SwiftFiles). It is data rather than code, beside Brands.json, so a
// spelling joins as a reviewed decision about the house made once for every engine, never a project's
// allowedSegments entry.
//
//go:embed KeywordTerms.json
var keywordTermsFile []byte

// KeywordTermVocabulary is KeywordTerms.json, read and checked.
type KeywordTermVocabulary struct {
	About        string        `json:"about"`
	KeywordTerms []KeywordTerm `json:"keywordTerms"`
}

// KeywordTerm is one entry of the vocabulary: the keyword as its language spells it, the language, the names in
// which the capitalized keyword means the keyword, why, and a page where the language spells it.
type KeywordTerm struct {
	Term      string   `json:"term"`
	Language  string   `json:"language"`
	Spellings []string `json:"spellings"`
	Reason    string   `json:"reason"`
	Citation  string   `json:"citation"`
}

// KeywordTerms is the embedded file, read once. An invalid file panics at startup, since a vocabulary read wrongly
// would silence the abbreviation rule on names the list never admitted while its tests reported it clean.
var KeywordTerms = mustLoadKeywordTerms(keywordTermsFile)

func mustLoadKeywordTerms(data []byte) KeywordTermVocabulary {
	vocabulary, loadError := loadKeywordTerms(data)
	if loadError != nil {
		// Invariant: the file is embedded and its loader is tested, so a refusal here is a broken build of policy,
		// and running with a vocabulary read wrongly would pass names the list never admitted.
		panic(fmt.Sprintf("KeywordTerms.json: %v", loadError))
	}
	return vocabulary
}

// loadKeywordTerms reads the file, refusing an unknown key, a file that does not state its bar, a term listed twice,
// and an entry whose shape could be read as something other than meant. Whether the term is an abbreviation the
// vocabulary judges, and judges with advice, is internal/naming/abbreviation's to check, since it reads both files.
func loadKeywordTerms(data []byte) (KeywordTermVocabulary, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var vocabulary KeywordTermVocabulary
	if decodeError := decoder.Decode(&vocabulary); decodeError != nil {
		return KeywordTermVocabulary{}, decodeError
	}
	if strings.TrimSpace(vocabulary.About) == "" {
		return KeywordTermVocabulary{}, fmt.Errorf("about is empty, and the file states the bar every entry meets")
	}
	seen := make(map[string]bool, len(vocabulary.KeywordTerms))
	for _, entry := range vocabulary.KeywordTerms {
		if refusal := keywordTermRefusal(entry); refusal != "" {
			return KeywordTermVocabulary{}, fmt.Errorf("%q %s", entry.Term, refusal)
		}
		if seen[entry.Term] {
			return KeywordTermVocabulary{}, fmt.Errorf("%q appears twice", entry.Term)
		}
		seen[entry.Term] = true
	}
	return vocabulary, nil
}

// keywordTermRefusal is why an entry is refused, or empty when it is not.
//
// A term is lowercase letters a to z, as an abbreviation is, since it names the abbreviation entry it speaks for. A
// spelling opens with the term capitalized, as the segment form matches it, and then an uppercase letter, so it names
// the keyword and the word after it rather than the keyword alone, which would exempt every use of the letters. The
// rest is ASCII letters and digits, since it is part of an identifier.
func keywordTermRefusal(entry KeywordTerm) string {
	if entry.Term == "" || strings.TrimLeft(entry.Term, "abcdefghijklmnopqrstuvwxyz") != "" {
		return "is not lowercase letters a to z, as the abbreviation it speaks for is"
	}
	if strings.TrimSpace(entry.Language) == "" {
		return "names no language, and a keyword is a keyword of one"
	}
	if len(entry.Spellings) == 0 {
		return "lists no spellings, so it would read nothing as the keyword"
	}
	capitalized := strings.ToUpper(entry.Term[:1]) + entry.Term[1:]
	seen := make(map[string]bool, len(entry.Spellings))
	for _, spelling := range entry.Spellings {
		rest, opensWithTerm := strings.CutPrefix(spelling, capitalized)
		if !opensWithTerm || rest == "" || rest[0] < 'A' || rest[0] > 'Z' {
			return fmt.Sprintf(
				"lists %q, which does not open with %q and then a new word, and every spelling names the next word",
				spelling,
				capitalized,
			)
		}
		for index := 1; index < len(rest); index++ {
			character := rest[index]
			isLetterOrDigit := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9')
			if !isLetterOrDigit {
				return fmt.Sprintf("lists %q, which holds something other than letters and digits", spelling)
			}
		}
		if seen[spelling] {
			return fmt.Sprintf("lists %q twice", spelling)
		}
		seen[spelling] = true
	}
	if strings.TrimSpace(entry.Reason) == "" {
		return "gives no reason, and every entry says why its letters are a keyword and not an abbreviation"
	}
	citation, parseError := url.Parse(entry.Citation)
	if entry.Citation == "" || parseError != nil || citation.Scheme != "https" || citation.Host == "" {
		return "cites no https page where its language spells the keyword, and every entry cites one"
	}
	return ""
}
