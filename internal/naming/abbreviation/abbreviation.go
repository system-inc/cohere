// Package abbreviation is the house's abbreviation vocabulary, read from cohere's policy/Abbreviations.json rather
// than written here as code, and the one reading of it every Go-hosted engine shares.
//
// It is data because three engines read it. The TypeScript rule (nexus/consistency-no-abbreviated-identifier), the
// Swift engine's naming rules and the Go engine's rule must judge the same words, so the list lives in one file
// under policy/, and the interpretation of that file lives here, once, with no engine's syntax tree in sight. Each
// entry carries its reason with it, so the file can be pruned by someone who was not there when it was written.
//
// What stays with each engine is policy: the TypeScript rule's framework exemptions and its rule.Message, the Go
// rule's reading of which names are its own. A finding here is a message id and the values its text needs, so each
// engine renders the same ids with the same values from its own rule's entry in the policy message catalog.
//
// # Order is part of the data
//
// Within each form the first matching entry reports, and that is observable: `timeoutMsRef` reports the millisecond
// form because `ms` precedes `ref` among the suffixes. The phases run in a fixed order in Find, and within a phase
// the file's order is the order. The move from Go literals to this file kept every phase's order and was checked by
// linting 82,704 names, every distinct identifier in the ahra tree plus every abbreviation in every form, with the
// binary before and after: the findings were byte-identical.
//
// # Two guards the literals carried, and why they did not come along
//
// Each prefix had an "already spelled" guard and each suffix an "unless it ends with" list, so that `database` would
// not read as `db` and `parsedValue` not as `Val`. Neither could ever fire. A prefix matches only when an uppercase
// letter follows the abbreviation (`^db[A-Z]`), and every expansion continues in lowercase (`database`), so no name
// both matched and was already spelled. A suffix guard needs a name ending in both `Val` and `Value`, which no string
// does. Checked over every entry rather than reasoned per word, then measured: dropping all of them left the
// 82,704-name run byte-identical. They were not moved into the data, because Swift would have copied them and
// someone would have tried to tune a guard that changes nothing.
package abbreviation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/system-inc/cohere/policy"
)

// Form is where in a name an entry matched. Its values are the file's own keys for the forms, so a finding names the
// form as the vocabulary spells it.
type Form string

const (
	// FormWhole is a name that is the abbreviation entire.
	FormWhole Form = "whole"
	// FormPrefix is a camelCase name that begins with the abbreviation.
	FormPrefix Form = "prefix"
	// FormSuffix is a name that ends with the abbreviation capitalized.
	FormSuffix Form = "suffix"
	// FormSegment is the abbreviation capitalized as a camelCase word anywhere in a name.
	FormSegment Form = "segment"
)

// The ids of the four messages a finding can carry, one per shape it takes. They are the ids in
// policy/messages/consistency-no-abbreviated-identifier.json, which every engine's rule renders from. The id names
// the shape, not the word: the word is already in the text, and the vocabulary's entries carry no id.
const (
	// AbbreviatedIdentifier is every whole-word and prefix finding, with the values `name` and `advice`.
	AbbreviatedIdentifier = "abbreviatedIdentifier"
	// AbbreviatedSuffix is a suffix finding, with the values `name`, `suffix` and `advice`.
	AbbreviatedSuffix = "abbreviatedSuffix"
	// MillisecondSuffix is the millisecond unit written as a camelCase word, with the values `name` and
	// `suggestion`.
	MillisecondSuffix = "millisecondSuffix"
	// AbbreviatedWordSegment is a segment finding, with the values `name`, `word` and `suggestion`.
	AbbreviatedWordSegment = "abbreviatedWordSegment"
)

// vocabularyFile is the file's shape. Unknown keys are refused when it is read, so a misspelled form is a startup
// failure rather than an entry that silently never matches.
type vocabularyFile struct {
	About           string           `json:"about"`
	Abbreviations   []Entry          `json:"abbreviations"`
	AllowedNames    []allowedName    `json:"allowedNames"`
	AllowedSegments []allowedSegment `json:"allowedSegments"`
}

// Entry is one abbreviated word and the forms it is judged in.
type Entry struct {
	Abbreviation string `json:"abbreviation"`
	// Expansion is the word the abbreviation stands in for. Absent for a word that has no single expansion, where
	// Advice says what to write instead.
	Expansion string `json:"expansion"`
	// Advice replaces "Use <expansion>" in the whole-word message, and in the prefix message when the prefix form's
	// style is advice.
	Advice  string       `json:"advice"`
	Reason  string       `json:"reason"`
	Whole   *WholeForm   `json:"whole"`
	Prefix  *PrefixForm  `json:"prefix"`
	Suffix  *SuffixForm  `json:"suffix"`
	Segment *SegmentForm `json:"segment"`
}

// WholeForm judges a name that is the abbreviation entire.
type WholeForm struct {
	// Style is "orDescriptive" (use the expansion or a more descriptive name), "plain" (use the expansion), or
	// "advice" (the entry's advice).
	Style string `json:"style"`
}

// PrefixForm judges a camelCase name that begins with the abbreviation.
type PrefixForm struct {
	// Phase is "early", judged before any suffix, or "late", judged after them.
	Phase string `json:"phase"`
	// Style is empty for a rename to the expansion, or "advice" for the entry's advice.
	Style string `json:"style"`
}

// SuffixForm judges a name that ends with the abbreviation capitalized.
type SuffixForm struct {
	// Advice replaces the rename suggestion.
	Advice string `json:"advice"`
	// Matcher replaces the plain suffix test. The only one is "millisecondWord".
	Matcher string `json:"matcher"`
	// Replacement overrides the capitalized expansion as the text the suggestion writes.
	Replacement string `json:"replacement"`
}

// SegmentForm judges the abbreviation capitalized as a camelCase word anywhere in a name.
type SegmentForm struct{}

type allowedName struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type allowedSegment struct {
	Segment string `json:"segment"`
	Reason  string `json:"reason"`
}

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

// wordSegmentPatterns are the two shapes a mid-name segment can take, plus the replacement form.
type wordSegmentPatterns struct {
	boundary *regexp.Regexp
	camel    *regexp.Regexp
	replace  *regexp.Regexp
}

// millisecondSegmentPattern matches a millisecond unit written as a camelCase word.
//
// Go's regexp has no lookahead, so where the TypeScript wrote `[a-z]Ms(?=$|[A-Z])` this captures the character after
// instead and puts it back in the replacement. The distinction the pattern is making is unchanged: a lowercase
// letter before, and either the end of the name or the start of the next word after.
var millisecondSegmentPattern = regexp.MustCompile(`([a-z])Ms($|[A-Z])`)

// Policy is policy's file, read once. An invalid file panics at startup, which is the loud failure: a vocabulary
// read wrongly would make every engine quietly judge less than it appears to.
var Policy = mustLoad(policy.AbbreviationsFile())

func mustLoad(data []byte) *Vocabulary {
	loaded, loadError := Load(data)
	if loadError != nil {
		// Invariant: the file is embedded and its tests load it, so a refusal here is a broken build, not a state
		// any run can recover from.
		panic(fmt.Sprintf("policy/Abbreviations.json: %v", loadError))
	}
	return loaded
}

// Load reads a vocabulary file, refusing an unknown key and any entry the engines would read as something other
// than meant.
func Load(data []byte) (*Vocabulary, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file vocabularyFile
	if decodeError := decoder.Decode(&file); decodeError != nil {
		return nil, decodeError
	}

	loaded := &Vocabulary{
		WholeByName:     map[string]*Entry{},
		AllowedNames:    map[string]bool{},
		prefixPatterns:  map[string]*regexp.Regexp{},
		segmentPatterns: map[string]wordSegmentPatterns{},
	}
	seen := map[string]bool{}
	for index := range file.Abbreviations {
		entry := &file.Abbreviations[index]
		if validateError := validateEntry(entry); validateError != nil {
			return nil, fmt.Errorf("entry %d (%q): %w", index, entry.Abbreviation, validateError)
		}
		if seen[entry.Abbreviation] {
			return nil, fmt.Errorf("entry %q appears twice", entry.Abbreviation)
		}
		seen[entry.Abbreviation] = true

		if entry.Whole != nil {
			loaded.WholeByName[entry.Abbreviation] = entry
		}
		if entry.Prefix != nil {
			// A bad entry is refused with its reason rather than panicking, so a malformed file reads as an error.
			prefixPattern, compileError := regexp.Compile(`^` + entry.Abbreviation + `[A-Z]`)
			if compileError != nil {
				return nil, fmt.Errorf("entry %q's prefix pattern: %w", entry.Abbreviation, compileError)
			}
			loaded.prefixPatterns[entry.Abbreviation] = prefixPattern
			if entry.Prefix.Phase == "early" {
				loaded.EarlyPrefixes = append(loaded.EarlyPrefixes, entry)
			} else {
				loaded.LatePrefixes = append(loaded.LatePrefixes, entry)
			}
		}
		if entry.Suffix != nil {
			loaded.Suffixes = append(loaded.Suffixes, entry)
		}
		if entry.Segment != nil {
			word := Capitalize(entry.Abbreviation)
			loaded.Segments = append(loaded.Segments, entry)
			boundary, boundaryError := regexp.Compile(`(^|[^a-zA-Z])` + word + `($|[A-Z0-9])`)
			camel, camelError := regexp.Compile(`[a-z0-9]` + word + `($|[A-Z0-9])`)
			replace, replaceError := regexp.Compile(word + `($|[A-Z0-9])`)
			if compileError := errors.Join(boundaryError, camelError, replaceError); compileError != nil {
				return nil, fmt.Errorf("entry %q's segment patterns: %w", entry.Abbreviation, compileError)
			}
			loaded.segmentPatterns[entry.Abbreviation] = wordSegmentPatterns{
				boundary: boundary,
				camel:    camel,
				replace:  replace,
			}
		}
	}
	for _, allowed := range file.AllowedNames {
		loaded.AllowedNames[allowed.Name] = true
	}
	for _, allowed := range file.AllowedSegments {
		loaded.AllowedSegments = append(loaded.AllowedSegments, allowed.Segment)
	}
	return loaded, nil
}

// validateEntry refuses an entry the engines would read as something other than meant.
func validateEntry(entry *Entry) error {
	if entry.Abbreviation == "" || strings.ToLower(entry.Abbreviation) != entry.Abbreviation {
		return fmt.Errorf("the abbreviation must be lowercase and present")
	}
	if entry.Whole == nil && entry.Prefix == nil && entry.Suffix == nil && entry.Segment == nil {
		return fmt.Errorf("no form, so the entry judges nothing")
	}
	needsExpansion := false
	if entry.Whole != nil {
		switch entry.Whole.Style {
		case "orDescriptive", "plain":
			needsExpansion = true
		case "advice":
			if entry.Advice == "" {
				return fmt.Errorf("whole style is advice and the entry has none")
			}
		default:
			return fmt.Errorf("unknown whole style %q", entry.Whole.Style)
		}
	}
	if entry.Prefix != nil {
		if entry.Prefix.Phase != "early" && entry.Prefix.Phase != "late" {
			return fmt.Errorf("unknown prefix phase %q", entry.Prefix.Phase)
		}
		switch entry.Prefix.Style {
		case "":
			needsExpansion = true
		case "advice":
			if entry.Advice == "" {
				return fmt.Errorf("prefix style is advice and the entry has none")
			}
		default:
			return fmt.Errorf("unknown prefix style %q", entry.Prefix.Style)
		}
	}
	if entry.Suffix != nil {
		if entry.Suffix.Matcher != "" && entry.Suffix.Matcher != "millisecondWord" {
			return fmt.Errorf("unknown suffix matcher %q", entry.Suffix.Matcher)
		}
		if entry.Suffix.Advice == "" && entry.Suffix.Replacement == "" {
			needsExpansion = true
		}
	}
	if entry.Segment != nil {
		needsExpansion = true
	}
	if needsExpansion && entry.Expansion == "" {
		return fmt.Errorf("a form suggests the expansion and the entry has none")
	}
	return nil
}

// Capitalize is the spelling a suffix or segment form matches: `prop` becomes `Prop`.
func Capitalize(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}

// Finding is what the vocabulary says about a name before any engine's exemption.
type Finding struct {
	// Form and Entry are where the name matched and the word that matched. The TypeScript rule's framework
	// exemptions are keyed on the two together.
	Form  Form
	Entry *Entry
	// MessageId is one of the four message ids above, and Values are exactly the values its text uses.
	MessageId string
	Values    map[string]string
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
		suggestion := replaceFirst(patterns.replace, name, Capitalize(entry.Expansion)+"${1}")
		if suggestion == name {
			continue
		}
		return Finding{
			Form:      FormSegment,
			Entry:     entry,
			MessageId: AbbreviatedWordSegment,
			Values: map[string]string{
				"name":       name,
				"word":       Capitalize(entry.Abbreviation),
				"suggestion": suggestion,
			},
		}, true
	}

	return Finding{}, false
}

// wholeFinding is the finding for a name that is the abbreviation entire.
func wholeFinding(entry *Entry, name string) Finding {
	advice := `Use "` + entry.Expansion + `" or a more descriptive name.`
	switch entry.Whole.Style {
	case "advice":
		advice = entry.Advice
	case "plain":
		advice = `Use "` + entry.Expansion + `".`
	}
	return abbreviatedIdentifierFinding(FormWhole, entry, name, advice)
}

// prefixFinding is the finding for a camelCase name that begins with the abbreviation.
//
// An advice-style prefix names the abbreviation rather than the name, so `argCount` reads `Identifier "arg" should
// not be abbreviated`. That is how the role words have always reported.
func prefixFinding(entry *Entry, name string) Finding {
	if entry.Prefix.Style == "advice" {
		return abbreviatedIdentifierFinding(FormPrefix, entry, entry.Abbreviation, entry.Advice)
	}
	suggestion := entry.Expansion + strings.TrimPrefix(name, entry.Abbreviation)
	return abbreviatedIdentifierFinding(FormPrefix, entry, name, `Use "`+suggestion+`".`)
}

// suffixFinding is the finding for a name that ends with the abbreviation capitalized.
func suffixFinding(entry *Entry, name string, suffix string) Finding {
	advice := entry.Suffix.Advice
	if advice == "" {
		replacement := entry.Suffix.Replacement
		if replacement == "" {
			replacement = Capitalize(entry.Expansion)
		}
		advice = `Use "` + strings.TrimSuffix(name, suffix) + replacement + `".`
	}
	return Finding{
		Form:      FormSuffix,
		Entry:     entry,
		MessageId: AbbreviatedSuffix,
		Values:    map[string]string{"name": name, "suffix": suffix, "advice": advice},
	}
}

// abbreviatedIdentifierFinding is the shape every whole-word and prefix finding takes.
func abbreviatedIdentifierFinding(form Form, entry *Entry, name string, advice string) Finding {
	return Finding{
		Form:      form,
		Entry:     entry,
		MessageId: AbbreviatedIdentifier,
		Values:    map[string]string{"name": name, "advice": advice},
	}
}

// replaceFirst replaces only the first match, which is what JavaScript's non-global String.replace does and what
// every suggestion in this vocabulary means.
//
// Go's ReplaceAllString would rewrite every occurrence, so `variablesForVars` would come back double-renamed while
// the original renamed one segment and left the reader to judge the rest.
func replaceFirst(pattern *regexp.Regexp, text string, replacement string) string {
	location := pattern.FindStringSubmatchIndex(text)
	if location == nil {
		return text
	}
	expanded := pattern.ExpandString(nil, replacement, text, location)
	return text[:location[0]] + string(expanded) + text[location[1]:]
}
