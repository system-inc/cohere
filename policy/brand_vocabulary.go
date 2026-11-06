package policy

import "strings"

// BrandVocabulary is Brands.json, read and checked.
type BrandVocabulary struct {
	About  string  `json:"about"`
	Brands []Brand `json:"brands"`

	// longestFirst is every brand's spelling, the longest first, built when the file is read, so a name that opens
	// with two listed brands is read by the longer.
	longestFirst []string
}

// LeadingBrand is the listed brand name opens with, spelled exactly as listed, the longest when more than one is.
// What follows it is the caller's to judge.
func (vocabulary BrandVocabulary) LeadingBrand(name string) (string, bool) {
	for _, brand := range vocabulary.longestFirst {
		if strings.HasPrefix(name, brand) {
			return brand, true
		}
	}
	return "", false
}
