package nexus

import (
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
	"github.com/system-inc/cohere/policy"
)

// TestAbbreviationVocabularyCarriesEveryWordInOrder pins what the file must hold, form by form.
//
// These lists are the Go literals the vocabulary was moved out of, read off the rule at cohere
// d53901e, so the move is checked as count in and count out with the order kept. They are a second
// copy on purpose: an expectation derived from the file could not notice an entry dropped from it.
// Adding a word means adding it here too, which is the moment to decide where in its phase it runs.
//
// Order matters only where entries can both match one name, and the test pins all of it anyway:
// `ms` between `params` and `ref` is observable (`timeoutMsRef` reports the millisecond form), and
// `vars` before `var` decides which replacement a name gets.
func TestAbbreviationVocabularyCarriesEveryWordInOrder(test *testing.T) {
	test.Parallel()

	abbreviationsOf := func(entries []*abbreviation.Entry) []string {
		words := make([]string, 0, len(entries))
		for _, entry := range entries {
			words = append(words, entry.Abbreviation)
		}
		return words
	}
	wholeWords := make([]string, 0, len(vocabulary.WholeByName))
	for word := range vocabulary.WholeByName {
		wholeWords = append(wholeWords, word)
	}
	slices.Sort(wholeWords)

	cases := []struct {
		form string
		got  []string
		want []string
	}{
		// The switch (13), the early group (7), exactAbbreviations (10) and the thirteen Go words @system_cohere
		// added for every engine on #ec8mvbg. Whole-word matches cannot overlap, so this one is compared as a set.
		{"whole", wholeWords, sortedCopy([]string{
			"prop", "props", "param", "params", "ref", "config", "idx", "arg", "args", "acc", "char", "fn", "str",
			"ctx", "db", "tx", "opts", "cur", "pct", "prev",
			"val", "arr", "obj", "num", "res", "err", "req", "msg", "min", "max",
			"buf", "tmp", "pkg", "src", "dst", "conn", "cmd", "cfg", "pos", "wg", "mu", "ch", "sb",
		})},
		// The early group's loop, in its order.
		{"early prefix", abbreviationsOf(vocabulary.EarlyPrefixes), []string{
			"ctx", "db", "tx", "opts", "cur", "pct", "prev",
		}},
		// The late group (9), the role prefixes (3), prefixAbbreviations (10) and the thirteen Go words. No two
		// of these can match one name, so their relative order was never observable; the file's order is pinned.
		{"late prefix", abbreviationsOf(vocabulary.LatePrefixes), []string{
			"prop", "props", "param", "params", "ref", "config", "idx", "arg", "args", "char", "fn", "str",
			"val", "arr", "obj", "num", "res", "err", "req", "msg", "min", "max",
			"buf", "tmp", "pkg", "src", "dst", "conn", "cmd", "cfg", "pos", "wg", "mu", "ch", "sb",
		}},
		// The hand-written suffix chain (12), the millisecond check where it ran, and
		// suffixAbbreviations (10), in the chain's order.
		{"suffix", abbreviationsOf(vocabulary.Suffixes), []string{
			"prop", "props", "param", "params", "ms", "ref", "config", "idx", "arg", "args", "char", "fn", "str",
			"val", "arr", "obj", "num", "res", "err", "req", "msg", "min", "max",
		}},
		// wordSegmentAbbreviations (10), in order, then the thirteen Go words @system_cohere added for every engine
		// on #ec8mvbg, after them so no earlier entry's order moves.
		{"segment", abbreviationsOf(vocabulary.Segments), []string{
			"cwd", "dir", "env", "cli", "len", "seq", "db", "tx", "vars", "var",
			"buf", "tmp", "pkg", "src", "dst", "conn", "cmd", "cfg", "pos", "wg", "mu", "ch", "sb",
		}},
		{"allowed segment", vocabulary.AllowedSegments, []string{"InnoDb"}},
	}
	for _, testCase := range cases {
		test.Run(testCase.form, func(test *testing.T) {
			test.Parallel()
			if !slices.Equal(testCase.got, testCase.want) {
				test.Errorf(
					"%d entries %v, want %d %v",
					len(testCase.got),
					testCase.got,
					len(testCase.want),
					testCase.want,
				)
			}
		})
	}
	if len(vocabulary.AllowedNames) != 1 || !vocabulary.AllowedNames["URLSearchParams"] {
		test.Errorf("allowed names %v, want only URLSearchParams", vocabulary.AllowedNames)
	}
}

// TestAbbreviationVocabularyJudgesEveryForm runs one generated name through every form of every
// entry and checks the message id and the suggestion each produces.
//
// The table above proves the entries are present; this proves each one is wired to the branch it
// names, which a form read into the wrong table would fail.
func TestAbbreviationVocabularyJudgesEveryForm(test *testing.T) {
	test.Parallel()

	judged := 0
	for word := range vocabulary.WholeByName {
		finding, found := findAbbreviation(word)
		if !found || finding.form != "whole" || finding.message.Id != abbreviatedIdentifierText.Id {
			test.Errorf("whole %q: got %+v, want %s", word, finding, abbreviatedIdentifierText.Id)
		}
		judged++
	}
	for _, entry := range append(
		append([]*abbreviation.Entry{}, vocabulary.EarlyPrefixes...),
		vocabulary.LatePrefixes...,
	) {
		name := entry.Abbreviation + "Widget"
		finding, found := findAbbreviation(name)
		if !found || finding.form != "prefix" || finding.message.Id != abbreviatedIdentifierText.Id {
			test.Errorf("prefix %q: got %+v, want %s", name, finding, abbreviatedIdentifierText.Id)
		}
		judged++
	}
	for _, entry := range vocabulary.Suffixes {
		name := "widget" + abbreviation.Capitalize(entry.Abbreviation)
		wantId := abbreviatedSuffixText.Id
		if entry.Suffix.Matcher == "millisecondWord" {
			wantId = millisecondSuffixText.Id
		}
		finding, found := findAbbreviation(name)
		if !found || finding.form != "suffix" || finding.message.Id != wantId {
			test.Errorf("suffix %q: got %+v, want %s", name, finding, wantId)
		}
		judged++
	}
	for _, entry := range vocabulary.Segments {
		name := "widget" + abbreviation.Capitalize(entry.Abbreviation) + "Name"
		finding, found := findAbbreviation(name)
		if !found || finding.form != "segment" || finding.entry != entry {
			test.Errorf("segment %q: got %+v, want %s", name, finding, entry.Abbreviation)
		}
		judged++
	}
	// 43 whole, 42 prefix, 23 suffix, 23 segment: the counts the table above pins.
	if judged != 131 {
		test.Errorf("judged %d forms, want 131", judged)
	}
}

// TestAbbreviationSegmentsJudgeOnlyTheWholeWord: the short Go words are judged only as a whole camelCase word, so a
// name that holds their letters inside a longer word is not an abbreviation. `ch` must never read `Check`, nor `pos`
// read `Posix`, nor `buf` read `Buffer`.
func TestAbbreviationSegmentsJudgeOnlyTheWholeWord(test *testing.T) {
	test.Parallel()
	for _, name := range []string{
		"checkName", "widgetCheckName", "changeSet", "posixPath", "postOffice", "bufferSize", "widgetBuffer",
		"muteButton", "multiplier", "temporaryFile", "sourceMap", "commandLine", "packageName", "position",
	} {
		if finding, found := findAbbreviation(name); found {
			test.Errorf("%q reads as an abbreviation: %+v", name, finding)
		}
	}
	for _, name := range []string{"chList", "readCh", "posValue", "bufSize", "widgetMu", "sb"} {
		if _, found := findAbbreviation(name); !found {
			test.Errorf("%q holds a whole abbreviated word and was not judged", name)
		}
	}
}

// TestAbbreviationVocabularyRefusesAMalformedFile checks the loader fails loudly rather than
// reading a file into a vocabulary that quietly judges less.
func TestAbbreviationVocabularyRefusesAMalformedFile(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name string
		file string
	}{
		{"a misspelled form", `{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "suffx": {}}]}`},
		{"an entry with no form", `{"abbreviations": [{"abbreviation": "val", "expansions": ["value"]}]}`},
		{
			"an unknown whole style",
			`{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "whole": {"style": "loud"}}]}`,
		},
		{"a rename with no expansion", `{"abbreviations": [{"abbreviation": "val", "whole": {"style": "plain"}}]}`},
		{
			"an unknown prefix phase",
			`{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "prefix": {"phase": "middle"}}]}`,
		},
		{
			"an uppercase abbreviation",
			`{"abbreviations": [{"abbreviation": "Val", "expansions": ["value"], "segment": {}}]}`,
		},
		{
			"a duplicate entry",
			`{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "segment": {}}, ` +
				`{"abbreviation": "val", "expansions": ["value"], "segment": {}}]}`,
		},
		{
			"an unknown matcher",
			`{"abbreviations": [{"abbreviation": "ms", "expansions": ["m"], "suffix": {"matcher": "seconds"}}]}`,
		},
		{"a broken prefix", `{"abbreviations":[{"abbreviation":"v(","expansions":["v"],"prefix":{"phase":"late"}}]}`},
		{"a broken segment", `{"abbreviations":[{"abbreviation":"v(","expansions":["v"],"segment":{}}]}`},
		{
			"a retired per-word messageId",
			`{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], ` +
				`"whole": {"messageId": "noVal", "style": "plain"}}]}`,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			if _, loadError := abbreviation.Load([]byte(testCase.file)); loadError == nil {
				test.Errorf("the loader accepted it")
			}
		})
	}
	if _, loadError := abbreviation.Load(policy.AbbreviationsFile()); loadError != nil {
		test.Fatalf("policy's file itself is refused: %v", loadError)
	}
}

func sortedCopy(words []string) []string {
	copied := slices.Clone(words)
	slices.Sort(copied)
	return copied
}

// TestAbbreviationVocabularyOrderIsObservableWhereItMatters pins the two orderings a name can see.
//
// The order table above would also fail on a harmless reorder, so these say which reorders change a
// finding. `var` cannot match inside `Vars`, since the lowercase `s` is no word boundary, so `vars`
// before `var` is visible only in a name holding both words. No name in the ahra corpus does, which
// is why the 82,704-name run could not see a swap and this case exists.
func TestAbbreviationVocabularyOrderIsObservableWhereItMatters(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		wantId      string
		wantMessage string
	}{
		{"themeVarsVar", "abbreviatedWordSegment", `Use "themeVariablesVar".`},
		{"timeoutMsRef", "millisecondSuffix", `Use "timeoutInMillisecondsRef"`},
		{"dbVal", "abbreviatedIdentifier", `Use "databaseVal".`},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			finding, found := findAbbreviation(testCase.name)
			if !found || finding.message.Id != testCase.wantId {
				test.Fatalf("got %+v, want %s", finding, testCase.wantId)
			}
			if !strings.Contains(finding.message.Description, testCase.wantMessage) {
				test.Errorf("message %q does not contain %q", finding.message.Description, testCase.wantMessage)
			}
		})
	}
}
