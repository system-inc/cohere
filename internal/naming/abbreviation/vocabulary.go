package abbreviation

import (
	"slices"
	"strings"
)

// Vocabulary is the file turned into the tables each phase walks.
//
// The tables are exported for the engines that build their own lookups from them, like the TypeScript rule's
// candidate gate, and for the tests that pin their order. They are read, never written: every engine shares the one
// loaded vocabulary.
type Vocabulary struct {
	WholeByName     map[string]*Entry
	EarlyPrefixes   []*Entry
	LatePrefixes    []*Entry
	Suffixes        []*Entry
	Segments        []*Entry
	AllowedNames    map[string]bool
	AllowedSegments []string

	// segmentPatterns are the two shapes a mid-name segment can take, plus the replacement form, one per entry of
	// Segments and in its order, so Find reaches an entry's patterns by its index rather than by a lookup per name.
	segmentPatterns []wordSegmentPatterns
}

// beginsWithPrefix is `^<abbreviation>[A-Z]`, the prefix form, as a comparison: the name begins with the
// abbreviation and an ASCII capital follows it. Load accepts only letters a to z in an abbreviation, so the pattern
// holds no syntax and the two say the same.
func beginsWithPrefix(name string, abbreviation string) bool {
	return len(name) > len(abbreviation) && strings.HasPrefix(name, abbreviation) &&
		name[len(abbreviation)] >= 'A' && name[len(abbreviation)] <= 'Z'
}

// endsWithCapitalized is whether a name ends with the abbreviation capitalized, the suffix form, without building
// the capitalized word for every entry and every name. The abbreviation is letters a to z, so capitalizing it changes
// its first byte alone.
func endsWithCapitalized(name string, abbreviation string) bool {
	start := len(name) - len(abbreviation)
	return start >= 0 && name[start] == abbreviation[0]-'a'+'A' && name[start+1:] == abbreviation[1:]
}

// containsAllowedSegment reports whether a name contains a word that merely holds an abbreviation's letters, like
// `InnoDb`.
func (vocabulary *Vocabulary) containsAllowedSegment(name string) bool {
	for _, segment := range vocabulary.AllowedSegments {
		if strings.Contains(name, segment) {
			return true
		}
	}
	return false
}

// allowedSegmentHolds reports whether an allowed segment in the name contains this abbreviation's letters, which is
// what exempts `db` from an `InnoDb` name's early prefix check.
func (vocabulary *Vocabulary) allowedSegmentHolds(name string, abbreviation string) bool {
	for _, segment := range vocabulary.AllowedSegments {
		if strings.Contains(name, segment) && strings.Contains(strings.ToLower(segment), abbreviation) {
			return true
		}
	}
	return false
}

// Find answers what the vocabulary reports for a name, if anything, in the phase order the rule has always used:
// whole word, early prefix, suffix, late prefix, segment.
//
// It knows nothing about where the name sits. An engine asks it only after its own skips, and then silences its own
// exemptions by the form and word it returns.
//
// Every name an engine judges meets every entry, and nearly every name matches none, so each form is tested first
// as the literal text its pattern holds, and only a name that passes costs a regular expression run. A literal test
// that fails is a pattern that could not have matched, so the findings are the ones the patterns alone gave
// (#300qvxj), and find_equivalence_test.go holds them to that over every name in this repository and the ahra tree.
func (vocabulary *Vocabulary) Find(name string) (Finding, bool) {
	if vocabulary.AllowedNames[name] {
		return Finding{}, false
	}

	if entry, found := vocabulary.WholeByName[name]; found {
		return wholeFinding(entry, name), true
	}

	for _, entry := range vocabulary.EarlyPrefixes {
		if !beginsWithPrefix(name, entry.Abbreviation) {
			continue
		}
		if vocabulary.allowedSegmentHolds(name, entry.Abbreviation) {
			continue
		}
		return prefixFinding(entry, name), true
	}

	for _, entry := range vocabulary.Suffixes {
		if entry.Suffix.Matcher == "millisecondWord" {
			// The pattern holds `Ms` literally, so a name without it cannot match.
			if strings.Contains(name, "Ms") && millisecondSegmentPattern.MatchString(name) {
				replacement := "${1}" + entry.Suffix.Replacement + "${2}"
				suggestion := replaceFirst(millisecondSegmentPattern, name, replacement)
				return Finding{
					Form:      FormSuffix,
					Entry:     entry,
					MessageId: MillisecondSuffix,
					Values:    map[string]string{"name": name, "suggestion": suggestion},
					Spellings: []string{suggestion},
				}, true
			}
			continue
		}
		if !endsWithCapitalized(name, entry.Abbreviation) {
			continue
		}
		return suffixFinding(entry, name, Capitalize(entry.Abbreviation)), true
	}

	for _, entry := range vocabulary.LatePrefixes {
		if !beginsWithPrefix(name, entry.Abbreviation) {
			continue
		}
		return prefixFinding(entry, name), true
	}

	if vocabulary.containsAllowedSegment(name) {
		return Finding{}, false
	}

	for index, entry := range vocabulary.Segments {
		patterns := vocabulary.segmentPatterns[index]
		if !strings.Contains(name, patterns.word) {
			continue
		}
		if !patterns.boundary.MatchString(name) && !patterns.camel.MatchString(name) {
			continue
		}
		advice := entry.Advice
		var suggestions []string
		if entry.Segment.Style != "advice" {
			suggestions = expandedNames(entry, func(expansion string) string {
				return replaceFirst(patterns.replace, name, Capitalize(expansion)+"${1}")
			})
			// Every expansion rewrites the same occurrence, so a rewrite that leaves the name as it was leaves
			// it so for all of them, and the name holds nothing to rename.
			if slices.Contains(suggestions, name) {
				continue
			}
			advice = useAdvice(suggestions)
		}
		return Finding{
			Form:      FormSegment,
			Entry:     entry,
			MessageId: AbbreviatedWordSegment,
			Values: map[string]string{
				"name":   name,
				"word":   Capitalize(entry.Abbreviation),
				"advice": advice,
			},
			Spellings: suggestions,
		}, true
	}

	return Finding{}, false
}
