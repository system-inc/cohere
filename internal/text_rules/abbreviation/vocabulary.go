package abbreviation

import (
	"slices"
	"strings"
)

// Vocabulary is the file turned into the tables each phase reads: Find walks the suffix and segment tables in order,
// and answers both prefix phases from an index of EarlyPrefixes and LatePrefixes by abbreviation (prefixEntry), never
// walking those two.
//
// The tables are exported for the languages that build their own lookups from them, like the TypeScript rule's
// candidate gate, and for the tests that pin their order. They are read, never written: every language shares the one
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

	// prefixByAbbreviation is every entry of EarlyPrefixes and LatePrefixes by its abbreviation, and longestPrefix the
	// longest of those, so Find looks up the one prefix a name could begin with (prefixEntry) rather than comparing
	// the name with every entry.
	prefixByAbbreviation map[string]*Entry
	longestPrefix        int
}

// prefixEntry is the entry whose prefix form, `^<abbreviation>[A-Z]`, the name matches, nil when none does.
//
// Load accepts only letters a to z in an abbreviation, so a name that begins with one and then an ASCII capital has
// the abbreviation as its whole leading run of lowercase letters, ended by that capital. That run is the one
// abbreviation the name could begin with, and Load refuses an abbreviation that appears twice, so at most one entry
// matches, in either phase: the lookup gives exactly the entry the walk over EarlyPrefixes or LatePrefixes stopped at.
func (vocabulary *Vocabulary) prefixEntry(name string) *Entry {
	for index := 0; index < len(name) && index <= vocabulary.longestPrefix; index++ {
		if isAsciiLowercase(name[index]) {
			continue
		}
		if index == 0 || !isAsciiUppercase(name[index]) {
			return nil
		}
		return vocabulary.prefixByAbbreviation[name[:index]]
	}
	return nil
}

// endsWithCapitalized is whether a name ends with the abbreviation capitalized, the suffix form, without building
// the capitalized word for every entry and every name. The abbreviation is letters a to z, so capitalizing it changes
// its first byte alone.
func endsWithCapitalized(name string, abbreviation string) bool {
	start := len(name) - len(abbreviation)
	return start >= 0 && name[start] == abbreviation[0]-'a'+'A' && name[start+1:] == abbreviation[1:]
}

// ContainsAllowedSegment reports whether a name contains a word that merely holds an abbreviation's letters, like
// `InnoDb`. internal/text_rules/file_name_words asks it of a folder's name read with its parent's, so a brand spelled
// across the two, google/docs, is read as the brand.
func (vocabulary *Vocabulary) ContainsAllowedSegment(name string) bool {
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

// everySegmentIsKeywordTerm reports whether each place the word stands as a segment of the name, as the boundary and
// camel patterns read it, opens one of its keyword term's spellings, so `isVarDeclaration` holds nothing to report
// and `VarDeclarationForThemeVar` still does. It is asked only once a pattern has matched, so there is a segment.
//
// A segment begins at the name's start or after anything but a capital, which is `(^|[^a-zA-Z])` and `[a-z0-9]`
// together, and ends at the name's end, a capital or a digit.
func everySegmentIsKeywordTerm(name string, patterns wordSegmentPatterns) bool {
	for start := strings.Index(name, patterns.word); start >= 0; {
		after := start + len(patterns.word)
		beginsWord := start == 0 || !isAsciiUppercase(name[start-1])
		endsWord := after == len(name) || isAsciiUppercase(name[after]) || (name[after] >= '0' && name[after] <= '9')
		if beginsWord && endsWord && !opensKeywordTerm(name[start:], patterns.keywordTermSpellings) {
			return false
		}
		next := strings.Index(name[start+1:], patterns.word)
		if next < 0 {
			break
		}
		start += 1 + next
	}
	return true
}

// opensKeywordTerm reports whether text opens with one of the spellings at a word boundary: the end, or anything but
// a lowercase letter, so `VarScoped` opens `VarScopedDeclarations` and not `VarScopedness`.
func opensKeywordTerm(text string, spellings []string) bool {
	for _, spelling := range spellings {
		if strings.HasPrefix(text, spelling) && (len(text) == len(spelling) || !isAsciiLowercase(text[len(spelling)])) {
			return true
		}
	}
	return false
}

func isAsciiUppercase(character byte) bool {
	return character >= 'A' && character <= 'Z'
}

// containsAsciiUppercase reports whether any byte of the name is a capital A to Z. A byte of a multibyte rune is
// never one, so a non-ASCII capital, which no form's literal text holds, does not count.
func containsAsciiUppercase(name string) bool {
	for index := range len(name) {
		if isAsciiUppercase(name[index]) {
			return true
		}
	}
	return false
}

func isAsciiLowercase(character byte) bool {
	return character >= 'a' && character <= 'z'
}

// Find answers what the vocabulary reports for a name, if anything, in the phase order the rule has always used:
// whole word, early prefix, suffix, late prefix, segment.
//
// It knows nothing about where the name sits. A language asks it before or after its own skips, whichever is cheaper,
// and silences its own exemptions by the form and word it returns.
//
// Every name a language judges meets every entry, and nearly every name matches none, so each form is tested first
// as the literal text its pattern holds, and only a name that passes costs a regular expression run. A literal test
// that fails is a pattern that could not have matched, so the findings are the ones the patterns alone gave
// (#300qvxj), and find_equivalence_test.go holds them to that over every name in this repository and the ahra tree.
// The prefix forms are not walked at all: the one entry a name could begin with is looked up (prefixEntry), which
// prefix_index_test.go holds to the walk it replaced (#fex6s0v).
func (vocabulary *Vocabulary) Find(name string) (Finding, bool) {
	if vocabulary.AllowedNames[name] {
		return Finding{}, false
	}

	if entry, found := vocabulary.WholeByName[name]; found {
		return wholeFinding(entry, name), true
	}

	// Past the whole word every form needs an ASCII capital in the name, so a name with none, most of the names a
	// language declares, finds nothing and is answered here. Load accepts only letters a to z in an abbreviation, which
	// is what makes each form's capital certain: a prefix ends at one (prefixEntry), a suffix and a segment are the
	// abbreviation capitalized, the millisecond word holds `Ms`, and an allowed segment only ever says nothing is
	// found. find_equivalence_test.go and prefix_index_test.go hold this to the Finds without it.
	if !containsAsciiUppercase(name) {
		return Finding{}, false
	}

	prefix := vocabulary.prefixEntry(name)
	if prefix != nil && prefix.Prefix.Phase == "early" && !vocabulary.allowedSegmentHolds(name, prefix.Abbreviation) {
		return prefixFinding(prefix, name), true
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

	if prefix != nil && prefix.Prefix.Phase == "late" {
		return prefixFinding(prefix, name), true
	}

	if vocabulary.ContainsAllowedSegment(name) {
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
		if len(patterns.keywordTermSpellings) > 0 && everySegmentIsKeywordTerm(name, patterns) {
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
