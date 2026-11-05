package abbreviation_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
)

// Every finding carries exactly the values its message's text uses, since each engine renders the id it names with
// those values and a missing one would print its placeholder. Run over one generated name per form of every entry,
// the same names nexus's tests judge, so no form of any word goes unchecked.
func TestFindGivesEachMessageItsValues(test *testing.T) {
	test.Parallel()
	valuesByMessage := map[string][]string{
		abbreviation.AbbreviatedIdentifier:  {"advice", "name"},
		abbreviation.AbbreviatedSuffix:      {"advice", "name", "suffix"},
		abbreviation.MillisecondSuffix:      {"name", "suggestion"},
		abbreviation.AbbreviatedWordSegment: {"name", "suggestion", "word"},
	}
	vocabulary := abbreviation.Policy
	names := slices.Collect(maps.Keys(vocabulary.WholeByName))
	for _, entry := range slices.Concat(vocabulary.EarlyPrefixes, vocabulary.LatePrefixes) {
		names = append(names, entry.Abbreviation+"Widget")
	}
	for _, entry := range vocabulary.Suffixes {
		names = append(names, "widget"+abbreviation.Capitalize(entry.Abbreviation))
	}
	for _, entry := range vocabulary.Segments {
		names = append(names, "widget"+abbreviation.Capitalize(entry.Abbreviation)+"Name")
	}
	for _, name := range names {
		finding, found := vocabulary.Find(name)
		if !found {
			test.Errorf("%q: not found", name)
			continue
		}
		want, known := valuesByMessage[finding.MessageId]
		if !known {
			test.Errorf("%q: message %q is none of the four", name, finding.MessageId)
			continue
		}
		if got := slices.Sorted(maps.Keys(finding.Values)); !slices.Equal(got, want) {
			test.Errorf("%q: values %v, want %v", name, got, want)
		}
	}
}

// A name holding none of the words is no finding, and an allowed name is none even though it holds one.
func TestFindStaysQuietOnFullWords(test *testing.T) {
	test.Parallel()
	for _, name := range []string{"requestContext", "readError", "position", "URLSearchParams", "InnoDbIndex"} {
		if finding, found := abbreviation.Policy.Find(name); found {
			test.Errorf("%q reads as an abbreviation: %+v", name, finding)
		}
	}
}
