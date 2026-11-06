package policy

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Brands.json is the vocabulary of brands whose owners spell them opening lowercase, iMessage as Apple spells it,
// which the casing rules read where they judge a name as UpperCamelCase (#vf4ytzg). It is data rather than code so a
// brand joins the list as a reviewed decision about the house, in one place, against the bar the file states. The
// Swift engine compiles in the same bytes (SwiftFiles).
//
//go:embed Brands.json
var brandsFile []byte

// Brand is one entry of the vocabulary: the brand as its owner spells it, the owner, and a page where the owner
// spells it so.
type Brand struct {
	Brand    string `json:"brand"`
	Owner    string `json:"owner"`
	Citation string `json:"citation"`
}

// Brands is the embedded file, read once. An invalid file panics at startup, since a vocabulary read wrongly would
// let a casing rule pass a name no owner spells while its tests reported it clean.
var Brands = mustLoadBrands(brandsFile)

func mustLoadBrands(data []byte) BrandVocabulary {
	vocabulary, loadError := loadBrands(data)
	if loadError != nil {
		// Invariant: the file is embedded and its loader is tested, so a refusal here is a broken build of policy,
		// and running with a vocabulary read wrongly would pass names the bar never admitted.
		panic(fmt.Sprintf("Brands.json: %v", loadError))
	}
	return vocabulary
}

// loadBrands reads the file, refusing an unknown key, a file that does not state its bar, a duplicate, and an entry
// that does not meet the bar as far as its shape can show: a brand that is not an identifier's opening or that opens
// with a capital, an entry with no owner, and one with no citation that is an https address.
func loadBrands(data []byte) (BrandVocabulary, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var vocabulary BrandVocabulary
	if decodeError := decoder.Decode(&vocabulary); decodeError != nil {
		return BrandVocabulary{}, decodeError
	}
	if strings.TrimSpace(vocabulary.About) == "" {
		return BrandVocabulary{}, fmt.Errorf("about is empty, and the file states the bar every entry meets")
	}
	seen := make(map[string]bool, len(vocabulary.Brands))
	for _, entry := range vocabulary.Brands {
		if refusal := brandRefusal(entry); refusal != "" {
			return BrandVocabulary{}, fmt.Errorf("%q %s", entry.Brand, refusal)
		}
		if seen[entry.Brand] {
			return BrandVocabulary{}, fmt.Errorf("%q appears twice", entry.Brand)
		}
		seen[entry.Brand] = true
		vocabulary.longestFirst = append(vocabulary.longestFirst, entry.Brand)
	}
	sort.SliceStable(vocabulary.longestFirst, func(left int, right int) bool {
		return len(vocabulary.longestFirst[left]) > len(vocabulary.longestFirst[right])
	})
	return vocabulary, nil
}

// brandRefusal is why an entry is refused, or empty when it is not. A brand opens with a lowercase ASCII letter, since
// a brand opening with a capital is UpperCamelCase already and listing it would say nothing, and the rest is ASCII
// letters and digits, since it opens an identifier.
func brandRefusal(entry Brand) string {
	if entry.Brand == "" || entry.Brand[0] < 'a' || entry.Brand[0] > 'z' {
		return "does not open with a lowercase letter, and the list holds only brands whose owners spell them so"
	}
	for index := 1; index < len(entry.Brand); index++ {
		character := entry.Brand[index]
		isLetterOrDigit := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9')
		if !isLetterOrDigit {
			return "holds something other than letters and digits, so no identifier can open with it"
		}
	}
	if strings.TrimSpace(entry.Owner) == "" {
		return "names no owner, and the bar is the owner's own spelling"
	}
	citation, parseError := url.Parse(entry.Citation)
	if entry.Citation == "" || parseError != nil || citation.Scheme != "https" || citation.Host == "" {
		return "cites no https page where its owner spells it, and every entry cites one"
	}
	return ""
}
