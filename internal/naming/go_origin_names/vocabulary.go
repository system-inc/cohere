package go_origin_names

import (
	"strings"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
)

// Vocabulary is the abbreviation vocabulary as an error's name is built from it, by lowercased word: the words with
// one mechanical expansion, and the words a name holding one is left to a hand for.
type Vocabulary struct {
	// Expansions are the words with exactly one expansion and no advice, each to that expansion.
	Expansions map[string]string
	// AdviceWords have no mechanical expansion: a name holding one is judged by hand.
	AdviceWords map[string]bool
	// MatcherWords are judged by a matcher, not as a plain word (`ms`): a name holding one is judged by hand.
	MatcherWords map[string]bool
	// AllowedNames and AllowedSegments are the vocabulary's own: a function name that is or holds one names nothing.
	AllowedNames    map[string]bool
	AllowedSegments []string
}

// VocabularyOf reads an error name's vocabulary out of the shared one, so the rule and the codemod expand a function's
// words by the same file. An entry whose suffix has a matcher is a matcher word; one with more or fewer than one
// expansion, its own advice, or an advice-style whole or prefix form is an advice word; every other is an expansion.
func VocabularyOf(loaded *abbreviation.Vocabulary) Vocabulary {
	words := Vocabulary{
		Expansions:      map[string]string{},
		AdviceWords:     map[string]bool{},
		MatcherWords:    map[string]bool{},
		AllowedNames:    map[string]bool{},
		AllowedSegments: append([]string(nil), loaded.AllowedSegments...),
	}
	// Every entry has a form, so each is in at least one of the form tables, and an entry in several is read once per
	// table to the same verdict.
	entries := append([]*abbreviation.Entry(nil), loaded.EarlyPrefixes...)
	entries = append(entries, loaded.LatePrefixes...)
	entries = append(entries, loaded.Suffixes...)
	entries = append(entries, loaded.Segments...)
	for _, entry := range loaded.WholeByName {
		entries = append(entries, entry)
	}
	for _, entry := range entries {
		word := strings.ToLower(entry.Abbreviation)
		switch {
		case entry.Suffix != nil && entry.Suffix.Matcher != "":
			words.MatcherWords[word] = true
		case len(entry.Expansions) != 1 || entry.Advice != "" ||
			(entry.Whole != nil && entry.Whole.Style == "advice") ||
			(entry.Prefix != nil && entry.Prefix.Style == "advice"):
			words.AdviceWords[word] = true
		default:
			words.Expansions[word] = entry.Expansions[0]
		}
	}
	for name := range loaded.AllowedNames {
		words.AllowedNames[name] = true
	}
	return words
}
