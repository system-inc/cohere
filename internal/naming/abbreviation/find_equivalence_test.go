package abbreviation

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/system-inc/cohere/internal/corpus"
	"github.com/system-inc/cohere/policy"
)

// Find tests each form as literal text before it runs a pattern, and drops the prefix pattern for a comparison
// (#300qvxj). That is a claim that the literal tests only ever say no where the patterns would, so this holds the
// Find that ships to the Find it replaced, kept below as it was, over every name real code has written.
//
// The corpus is every word in this repository's own Go and TypeScript, the 82,367 identifiers from the ahra tree
// the nexus gate's test reads, and every abbreviation in every form a name can hold it in. A finding must agree
// field for field: the form, the entry, the message id and every value.
func TestFindAgreesWithTheRegularExpressionFind(test *testing.T) {
	test.Parallel()
	reference := newRegexpOnlyFind(test, Policy)
	names := equivalenceCorpus(test, Policy)
	disagreements := 0
	for _, name := range names {
		want, wantFound := reference.find(name)
		got, gotFound := Policy.Find(name)
		if gotFound == wantFound && got.Form == want.Form && got.Entry == want.Entry &&
			got.MessageId == want.MessageId && maps.Equal(got.Values, want.Values) {
			continue
		}
		disagreements++
		// The first few name the shape that broke; the count says how far it reaches.
		if disagreements <= 20 {
			test.Errorf("%q: got %v %+v, the regular expression Find gives %v %+v", name, gotFound, got, wantFound,
				want)
		}
	}
	if disagreements > 0 {
		test.Errorf("%d of %d names disagree", disagreements, len(names))
	}
	test.Logf("%d names compared", len(names))
}

// equivalenceCorpus is the names the two Finds are compared over, each once.
func equivalenceCorpus(test *testing.T, vocabulary *Vocabulary) []string {
	test.Helper()
	root, rootError := corpus.RepositoryRoot()
	if rootError != nil {
		test.Fatal(rootError)
	}
	names := map[string]bool{}
	// Words rather than parsed identifiers: a word in a comment or a string is a name someone could write, and a
	// larger corpus only makes the agreement stronger.
	isWordRune := func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$'
	}
	sourceFiles := 0
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			// Microsoft's TypeScript checkout is someone else's code, and git's own directory is no code at all.
			if entry.Name() == ".git" || entry.Name() == "node_modules" || path == filepath.Join(root, "TypeScript") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		sourceFiles++
		for _, word := range strings.FieldsFunc(string(contents), func(character rune) bool {
			return !isWordRune(character)
		}) {
			names[word] = true
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}

	identifiers, readError := os.ReadFile(filepath.Join(root, "internal", "lint", "rules", "nexus", "testdata",
		"identifiers.txt"))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, identifier := range strings.Split(strings.TrimSpace(string(identifiers)), "\n") {
		names[identifier] = true
	}

	abbreviations := slices.Collect(maps.Keys(vocabulary.WholeByName))
	for _, entries := range [][]*Entry{vocabulary.EarlyPrefixes, vocabulary.LatePrefixes, vocabulary.Suffixes,
		vocabulary.Segments} {
		for _, entry := range entries {
			abbreviations = append(abbreviations, entry.Abbreviation)
		}
	}
	for _, abbreviation := range abbreviations {
		word := Capitalize(abbreviation)
		for _, name := range []string{
			abbreviation, word, strings.ToUpper(abbreviation), abbreviation + "s", word + "s", abbreviation + "É",
			abbreviation + "Name", abbreviation + "name", abbreviation + "_name", abbreviation + "2",
			word + "Name", word + "name", word + "2", word + "é", "_" + abbreviation, "$" + abbreviation,
			"user" + word, "user" + word + "Name", "user" + word + "2", "user" + word + "s", "user" + word + "é",
			"user_" + abbreviation, "user_" + word, "User" + word, "USER" + word, "user2" + word, "é" + word,
			"Inno" + word, "inno" + word + "Index", "user" + strings.ToUpper(abbreviation),
		} {
			names[name] = true
		}
		// Two abbreviations together, which is where the phase order decides which one reports.
		for _, other := range abbreviations {
			names[abbreviation+Capitalize(other)] = true
			names[word+Capitalize(other)+"Name"] = true
		}
	}
	// Each keyword term's spellings where they read as the keyword, where they run on into a longer word, and beside a
	// bare use of the letters or another abbreviation.
	for _, keywordTerm := range policy.KeywordTerms.KeywordTerms {
		for _, spelling := range keywordTerm.Spellings {
			for _, name := range []string{
				spelling, "is" + spelling, spelling + "s", spelling + "ness", spelling + "2", spelling + "Name",
				"No" + spelling, "Top" + spelling, "TOP" + spelling, spelling + Capitalize(keywordTerm.Term),
				Capitalize(keywordTerm.Term) + spelling, spelling + "Src", "user" + spelling + "Cmd",
			} {
				names[name] = true
			}
		}
	}
	for name := range vocabulary.AllowedNames {
		names[name] = true
	}
	for _, segment := range vocabulary.AllowedSegments {
		names[segment] = true
		names[strings.ToLower(segment[:1])+segment[1:]+"Index"] = true
		names["read"+segment+"Name"] = true
	}

	// A corpus that failed to load would agree with anything.
	if sourceFiles < 1000 || len(names) < 100000 {
		test.Fatalf("read %d source files and %d names, too few to have read the repository", sourceFiles,
			len(names))
	}
	return slices.Sorted(maps.Keys(names))
}

// regexpOnlyFind is Find as it was before #300qvxj, every form tested by its regular expression alone, kept as the
// reference the shipped Find is held to. The patterns are built here from the vocabulary's tables, as Load built
// them then, so nothing the shipped Find reads stands in for them.
//
// Keyword terms (#hvyvfr5) are read a second way here: rather than walking each segment, every spelling that ends
// at a word boundary has its keyword rewritten to `Term`, and the segment patterns run on what is left, so a segment
// survives exactly where it is no keyword term. `Term` opens with a capital and ends in a lowercase letter, as the
// keyword capitalized does, so no other word's segment is made or lost by the rewrite, and it opens no spelling, so
// a bare keyword before it is not made into one: `Keyword` would turn `VarVarKeyword` into `VarKeywordKeyword`.
type regexpOnlyFind struct {
	vocabulary      *Vocabulary
	prefixPatterns  map[string]*regexp.Regexp
	segmentPatterns map[string]wordSegmentPatterns
	keywordTerms    map[string]*regexp.Regexp
}

func newRegexpOnlyFind(test *testing.T, vocabulary *Vocabulary) regexpOnlyFind {
	test.Helper()
	reference := regexpOnlyFind{
		vocabulary:      vocabulary,
		prefixPatterns:  map[string]*regexp.Regexp{},
		segmentPatterns: map[string]wordSegmentPatterns{},
		keywordTerms:    map[string]*regexp.Regexp{},
	}
	for _, keywordTerm := range policy.KeywordTerms.KeywordTerms {
		alternatives := make([]string, len(keywordTerm.Spellings))
		for index, spelling := range keywordTerm.Spellings {
			alternatives[index] = regexp.QuoteMeta(strings.TrimPrefix(spelling, Capitalize(keywordTerm.Term)))
		}
		reference.keywordTerms[keywordTerm.Term] = regexp.MustCompile(
			Capitalize(keywordTerm.Term) + `((?:` + strings.Join(alternatives, "|") + `)(?:$|[^a-z]))`,
		)
	}
	for _, entry := range slices.Concat(vocabulary.EarlyPrefixes, vocabulary.LatePrefixes) {
		reference.prefixPatterns[entry.Abbreviation] = regexp.MustCompile(`^` + entry.Abbreviation + `[A-Z]`)
	}
	for _, entry := range vocabulary.Segments {
		word := Capitalize(entry.Abbreviation)
		reference.segmentPatterns[entry.Abbreviation] = wordSegmentPatterns{
			boundary: regexp.MustCompile(`(^|[^a-zA-Z])` + word + `($|[A-Z0-9])`),
			camel:    regexp.MustCompile(`[a-z0-9]` + word + `($|[A-Z0-9])`),
			replace:  regexp.MustCompile(word + `($|[A-Z0-9])`),
		}
	}
	return reference
}

func (reference regexpOnlyFind) find(name string) (Finding, bool) {
	vocabulary := reference.vocabulary
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
		if reference.prefixPatterns[entry.Abbreviation].MatchString(name) {
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
		if !reference.prefixPatterns[entry.Abbreviation].MatchString(name) {
			continue
		}
		return prefixFinding(entry, name), true
	}

	if vocabulary.containsAllowedSegment(name) {
		return Finding{}, false
	}

	for _, entry := range vocabulary.Segments {
		patterns := reference.segmentPatterns[entry.Abbreviation]
		judged := name
		if keywordTerm, isKeyword := reference.keywordTerms[entry.Abbreviation]; isKeyword {
			// The match takes the character after a spelling, which may open the next one, so it runs until
			// nothing is left to rewrite.
			for rewritten := keywordTerm.ReplaceAllString(judged, "Term$1"); rewritten != judged; {
				judged = rewritten
				rewritten = keywordTerm.ReplaceAllString(judged, "Term$1")
			}
		}
		if !patterns.boundary.MatchString(judged) && !patterns.camel.MatchString(judged) {
			continue
		}
		advice := entry.Advice
		if entry.Segment.Style != "advice" {
			suggestions := expandedNames(entry, func(expansion string) string {
				return replaceFirst(patterns.replace, name, Capitalize(expansion)+"${1}")
			})
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
