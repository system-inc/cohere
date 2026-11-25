package abbreviation

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/system-inc/cohere/internal/corpus"
)

// prefixIndexCorpusVariable names directories whose Go files the index is also held to the walk over, separated as
// the platform separates a path list, so a run can reach a tree too large to keep in this repository.
const prefixIndexCorpusVariable = "COHERE_ABBREVIATION_CORPUS"

// Find looks up the one prefix entry a name could begin with rather than walking EarlyPrefixes and LatePrefixes
// (#fex6s0v). That is a claim that the lookup gives the very entry the walk stopped at, in each phase, so this holds
// the Find that ships to the Find it replaced, kept below as it was, field for field: found or not, the form, the entry
// itself, the message id, every value and every spelling. It holds both vocabularies, Policy and NameWords, which
// internal/file_name_words reads through the same Find.
//
// The names are every word of cohere-go rule's fixtures, the corpus find_equivalence_test.go reads for the
// vocabulary (this repository's words, the ahra tree's identifiers, and every abbreviation in every form), every word
// of every Go file under the directories COHERE_ABBREVIATION_CORPUS names, and each prefix entry's abbreviation
// followed by each allowed segment, as in `dbInnoDbRows`, the one shape that reaches an early prefix's allowed-segment
// exemption, which no code in either tree holds. A word is a run of letters, digits, underscores and dollar signs, so
// every identifier is one, and a word in a comment or a string is a name someone could write.
func TestFindAgreesWithThePrefixWalk(test *testing.T) {
	test.Parallel()
	root, rootError := corpus.RepositoryRoot()
	if rootError != nil {
		test.Fatal(rootError)
	}
	fixtureWords := wordsUnder(test, filepath.Join(root, "internal", "go_language", "rules",
		"consistency_no_abbreviated_identifier", "testdata"), func(string) bool { return true })
	// Fixtures that failed to read would agree with anything.
	if len(fixtureWords) < 500 {
		test.Fatalf("read %d words from the rule's fixtures, too few to have read them", len(fixtureWords))
	}
	test.Logf("the rule's fixtures: %d distinct words", len(fixtureWords))
	directoryWords := []string{}
	for _, directory := range filepath.SplitList(os.Getenv(prefixIndexCorpusVariable)) {
		if directory == "" {
			continue
		}
		words := wordsUnder(test, directory, func(path string) bool { return strings.HasSuffix(path, ".go") })
		test.Logf("%s: %d distinct words", directory, len(words))
		directoryWords = append(directoryWords, words...)
	}
	for vocabularyName, vocabulary := range map[string]*Vocabulary{"Policy": Policy, "NameWords": NameWords} {
		test.Run(vocabularyName, func(test *testing.T) {
			test.Parallel()
			names := map[string]bool{}
			for _, word := range slices.Concat(fixtureWords, directoryWords, equivalenceCorpus(test, vocabulary)) {
				names[word] = true
			}
			for _, entry := range slices.Concat(vocabulary.EarlyPrefixes, vocabulary.LatePrefixes) {
				for _, segment := range vocabulary.AllowedSegments {
					names[entry.Abbreviation+segment+"Rows"] = true
				}
			}
			requireAgreement(test, vocabulary, names)
		})
	}
}

// requireAgreement fails test for every name vocabulary's Find answers otherwise than walkingFind.
func requireAgreement(test *testing.T, vocabulary *Vocabulary, names map[string]bool) {
	test.Helper()
	disagreements := 0
	for _, name := range slices.Sorted(maps.Keys(names)) {
		want, wantFound := walkingFind(vocabulary, name)
		got, gotFound := vocabulary.Find(name)
		if gotFound == wantFound && got.Form == want.Form && got.Entry == want.Entry &&
			got.MessageId == want.MessageId && maps.Equal(got.Values, want.Values) &&
			slices.Equal(got.Spellings, want.Spellings) {
			continue
		}
		disagreements++
		// The first few name the shape that broke; the count says how far it reaches.
		if disagreements <= 20 {
			test.Errorf("%q: got %v %+v, the walking Find gives %v %+v", name, gotFound, got, wantFound, want)
		}
	}
	if disagreements > 0 {
		test.Errorf("%d of %d names disagree", disagreements, len(names))
	}
	test.Logf("%d names compared, %d disagree", len(names), disagreements)
}

// Find answers both prefix phases from its index and never from the tables it was built from: with the tables
// emptied every prefix finding stays, and with the index emptied every one is gone. A Find that walked the tables
// again would fail both halves.
func TestFindAnswersPrefixesFromItsIndex(test *testing.T) {
	test.Parallel()
	withoutTables := *Policy
	withoutTables.EarlyPrefixes = nil
	withoutTables.LatePrefixes = nil
	withoutIndex := *Policy
	withoutIndex.prefixByAbbreviation = nil
	for _, entry := range slices.Concat(Policy.EarlyPrefixes, Policy.LatePrefixes) {
		name := entry.Abbreviation + "Index"
		if finding, found := withoutTables.Find(name); !found || finding.Form != FormPrefix || finding.Entry != entry {
			test.Errorf("%q with the tables emptied: got %v %+v, want the prefix finding for %q", name, found, finding,
				entry.Abbreviation)
		}
		if finding, found := withoutIndex.Find(name); found && finding.Form == FormPrefix {
			test.Errorf("%q with the index emptied: got the prefix finding for %q, so Find walked the tables", name,
				finding.Entry.Abbreviation)
		}
	}
}

// wordsUnder is every distinct word of every file under a directory that keep accepts.
func wordsUnder(test *testing.T, directory string, keep func(path string) bool) []string {
	test.Helper()
	words := map[string]bool{}
	isWordRune := func(character rune) bool {
		return unicode.IsLetter(character) || unicode.IsDigit(character) || character == '_' || character == '$'
	}
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() || !keep(path) {
			return nil
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		for _, word := range strings.FieldsFunc(string(contents), func(character rune) bool {
			return !isWordRune(character)
		}) {
			words[word] = true
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return slices.Collect(maps.Keys(words))
}

// beginsWithPrefix is `^<abbreviation>[A-Z]`, the prefix form, as a comparison: the name begins with the
// abbreviation and an ASCII capital follows it. Load accepts only letters a to z in an abbreviation, so the pattern
// holds no syntax and the two say the same.
func beginsWithPrefix(name string, abbreviation string) bool {
	return len(name) > len(abbreviation) && strings.HasPrefix(name, abbreviation) &&
		name[len(abbreviation)] >= 'A' && name[len(abbreviation)] <= 'Z'
}

// walkingFind is Find as it was before #fex6s0v, each prefix phase a walk over its table in the file's order, kept
// as the reference the shipped Find is held to. Its other phases, whole word, suffix and segment, are Find's own,
// copied as they were, since #fex6s0v changed only the prefixes: a change to one of those in Find is made here too, or
// this test fails for a reason that is not the index.
func walkingFind(vocabulary *Vocabulary, name string) (Finding, bool) {
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
