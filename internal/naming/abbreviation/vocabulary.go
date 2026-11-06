package abbreviation

import (
	"regexp"
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

	// prefixPatterns is `^<abbreviation>[A-Z]`, built once per entry rather than per name.
	prefixPatterns map[string]*regexp.Regexp
	// segmentPatterns are the two shapes a mid-name segment can take, plus the replacement form.
	segmentPatterns map[string]wordSegmentPatterns
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
func (vocabulary *Vocabulary) Find(name string) (Finding, bool) {
	if vocabulary.AllowedNames[name] {
		return Finding{}, false
	}

	if entry, found := vocabulary.WholeByName[name]; found {
		return wholeFinding(entry, name), true
	}

	for _, entry := range vocabulary.EarlyPrefixes {
		if vocabulary.allowedSegmentHolds(name, entry.Abbreviation) {
			continue
		}
		if vocabulary.prefixPatterns[entry.Abbreviation].MatchString(name) {
			return prefixFinding(entry, name), true
		}
	}

	for _, entry := range vocabulary.Suffixes {
		if entry.Suffix.Matcher == "millisecondWord" {
			if millisecondSegmentPattern.MatchString(name) {
				replacement := "${1}" + entry.Suffix.Replacement + "${2}"
				suggestion := replaceFirst(millisecondSegmentPattern, name, replacement)
				return Finding{
					Form:      FormSuffix,
					Entry:     entry,
					MessageId: MillisecondSuffix,
					Values:    map[string]string{"name": name, "suggestion": suggestion},
				}, true
			}
			continue
		}
		suffix := Capitalize(entry.Abbreviation)
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		return suffixFinding(entry, name, suffix), true
	}

	for _, entry := range vocabulary.LatePrefixes {
		if !vocabulary.prefixPatterns[entry.Abbreviation].MatchString(name) {
			continue
		}
		return prefixFinding(entry, name), true
	}

	if vocabulary.containsAllowedSegment(name) {
		return Finding{}, false
	}

	for _, entry := range vocabulary.Segments {
		patterns := vocabulary.segmentPatterns[entry.Abbreviation]
		if !patterns.boundary.MatchString(name) && !patterns.camel.MatchString(name) {
			continue
		}
		advice := entry.Advice
		if entry.Segment.Style != "advice" {
			suggestions := expandedNames(entry, func(expansion string) string {
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
		}, true
	}

	return Finding{}, false
}
