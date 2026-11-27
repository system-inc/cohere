package abbreviation_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/text_rules/abbreviation"
)

// The synthetic names stand in for a recording of real code, so what they must hold is every shape real code could
// give an abbreviation: for every abbreviation, its capitalized word after each kind of character and before each
// kind, the grid a gate or a Find can lose a corner of. A recording held only the shapes someone had written; this
// holds the ones nobody has yet. Each cell is checked by reading the names, not by trusting the generator's lists, so
// a dropped opening or ending names the cell it emptied.
func TestSyntheticNamesHoldEveryBoundaryShape(test *testing.T) {
	test.Parallel()
	names := abbreviation.SyntheticNames(abbreviation.Policy)
	vocabulary := abbreviation.Policy
	abbreviations := map[string]bool{}
	for name := range vocabulary.WholeByName {
		abbreviations[name] = true
	}
	for _, entries := range [][]*abbreviation.Entry{vocabulary.EarlyPrefixes, vocabulary.LatePrefixes,
		vocabulary.Suffixes, vocabulary.Segments} {
		for _, entry := range entries {
			abbreviations[entry.Abbreviation] = true
		}
	}
	classes := []string{"Edge", "Lowercase", "Uppercase", "Digit", "Underscore", "Dollar", "NonAscii"}
	for word := range abbreviations {
		capitalized := abbreviation.Capitalize(word)
		held := map[[2]string]bool{}
		for _, name := range names {
			for start := strings.Index(name, capitalized); start >= 0; {
				before, _ := utf8.DecodeLastRuneInString(name[:start])
				after, _ := utf8.DecodeRuneInString(name[start+len(capitalized):])
				held[[2]string{boundaryClass(before), boundaryClass(after)}] = true
				next := strings.Index(name[start+1:], capitalized)
				if next < 0 {
					break
				}
				start += 1 + next
			}
		}
		for _, before := range classes {
			for _, after := range classes {
				if !held[[2]string{before, after}] {
					test.Errorf("no synthetic name holds %q after %s and before %s", capitalized, before, after)
				}
			}
		}
	}
	test.Logf("%d synthetic names", len(names))
}

// boundaryClass is the kind of character beside a word, utf8.RuneError at the name's edge reading as Edge.
func boundaryClass(character rune) string {
	switch {
	case character == utf8.RuneError:
		return "Edge"
	case character >= 'a' && character <= 'z':
		return "Lowercase"
	case character >= 'A' && character <= 'Z':
		return "Uppercase"
	case character >= '0' && character <= '9':
		return "Digit"
	case character == '_':
		return "Underscore"
	case character == '$':
		return "Dollar"
	default:
		return "NonAscii"
	}
}

// The clean names are where a gate's selectivity is measured, so each must be a name the vocabulary does not report,
// and there must be enough of them for a fraction of them to mean something.
func TestSyntheticCleanNamesAreCleanAndMany(test *testing.T) {
	test.Parallel()
	names := abbreviation.SyntheticCleanNames(abbreviation.Policy)
	for _, name := range names {
		if finding, reported := abbreviation.Policy.Find(name); reported {
			test.Errorf("%q is reported (%+v), so it is no clean name", name, finding.Values)
		}
	}
	if len(names) < 2000 {
		test.Fatalf("only %d clean names, too few to measure a gate by", len(names))
	}
	test.Logf("%d clean names", len(names))
}
