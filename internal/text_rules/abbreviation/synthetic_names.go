package abbreviation

import (
	"maps"
	"slices"
	"strings"
)

// syntheticOpenings are what SyntheticNames sets before an abbreviation: nothing, a word in each case, a digit, an
// underscore, a dollar sign and a letter outside ASCII. `Inno` is the opening of the allowed segment `InnoDb`, so a
// name holds the segment's letters around a word that is not its own.
var syntheticOpenings = []string{"", "user", "User", "USER", "user2", "user_", "_", "$", "é", "Inno", "inno"}

// syntheticEndings are what SyntheticNames sets after an abbreviation: nothing, a lowercase letter that runs the word
// on, a new word in each case, an underscore, a digit, a dollar sign, and a letter outside ASCII in each case.
var syntheticEndings = []string{"", "s", "Name", "name", "_name", "2", "$", "é", "É", "Index"}

// syntheticPlainWords are ordinary words a program names things with, none of them an abbreviation, joined to the
// vocabulary's expansions by SyntheticCleanNames.
var syntheticPlainWords = []string{
	"value", "count", "index", "name", "user", "order", "total", "first", "last", "result", "handler", "item", "list",
	"table", "row", "column", "label", "title", "entry", "record", "event", "state", "status", "output", "input",
	"path", "file", "line", "node", "page", "view", "size", "width", "height", "start", "finish", "next", "key",
	"color", "price", "amount", "date", "hour", "minute", "second", "button", "window", "layout", "theme", "cache",
	"queue", "stream", "token", "scope",
}

// SyntheticNames is every name the vocabulary's tests judge it by, sorted and each once: every abbreviation in every
// spelling, placed after and before every kind of character a name can hold beside it, every pair of abbreviations,
// each keyword term's spellings in and out of a longer word, and every allowed name and segment.
//
// It is generated from the vocabulary's own tables rather than recorded from some tree, so no boundary shape depends
// on someone having thought of it, or on some project having happened to write it. Each abbreviation is spelled three
// ways and set between each opening in syntheticOpenings and each ending in syntheticEndings, which is the full grid
// of what can stand on either side of a word: nothing, a lowercase letter, a capital, a digit, an underscore, a dollar
// sign and a letter outside ASCII. `elapsedMs` is a lowercase letter, the word, then the end, and `userMs` holds that
// boundary, the one splitting a name at its capitals loses.
func SyntheticNames(vocabulary *Vocabulary) []string {
	names := map[string]bool{}
	abbreviations := vocabulary.abbreviations()
	for _, abbreviation := range abbreviations {
		word := Capitalize(abbreviation)
		for _, spelling := range []string{abbreviation, word, strings.ToUpper(abbreviation)} {
			for _, opening := range syntheticOpenings {
				for _, ending := range syntheticEndings {
					names[opening+spelling+ending] = true
				}
			}
		}
		// Two abbreviations together, which is where the phase order decides which one reports.
		for _, other := range abbreviations {
			names[abbreviation+Capitalize(other)] = true
			names[word+Capitalize(other)+"Name"] = true
		}
	}
	// Each keyword term's spellings where they read as the keyword, where they run on into a longer word, and beside a
	// bare use of the letters or another abbreviation. The term is the abbreviation of the segment entry it speaks for.
	for index, entry := range vocabulary.Segments {
		for _, spelling := range vocabulary.segmentPatterns[index].keywordTermSpellings {
			for _, name := range []string{
				spelling, "is" + spelling, spelling + "s", spelling + "ness", spelling + "2", spelling + "Name",
				"No" + spelling, "Top" + spelling, "TOP" + spelling, spelling + Capitalize(entry.Abbreviation),
				Capitalize(entry.Abbreviation) + spelling, spelling + "Src", "user" + spelling + "Cmd",
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
	return slices.Sorted(maps.Keys(names))
}

// SyntheticCleanNames are names the vocabulary does not report that hold the letters its abbreviations are made of,
// sorted and each once: the words the vocabulary tells a writer to use instead, alone and joined to each other and to
// plain words, first word lowercase and capitalized. A gate in front of the vocabulary that admits many of these is
// admitting names for holding an expansion, which is the whole of what a writer was told to do.
func SyntheticCleanNames(vocabulary *Vocabulary) []string {
	expansions := []string{}
	for _, entries := range [][]*Entry{vocabulary.EarlyPrefixes, vocabulary.LatePrefixes, vocabulary.Suffixes,
		vocabulary.Segments} {
		for _, entry := range entries {
			expansions = append(expansions, entry.Expansions...)
		}
	}
	for _, entry := range vocabulary.WholeByName {
		expansions = append(expansions, entry.Expansions...)
	}
	candidates := map[string]bool{}
	for _, expansion := range expansions {
		candidates[expansion] = true
		candidates[Capitalize(expansion)] = true
		// Every name holds an expansion, so two plain words together, which share nothing with the vocabulary, are
		// never a clean name here.
		for _, other := range slices.Concat(expansions, syntheticPlainWords) {
			for _, pair := range [][2]string{{expansion, other}, {other, expansion}} {
				candidates[pair[0]+Capitalize(pair[1])] = true
				candidates[Capitalize(pair[0])+Capitalize(pair[1])] = true
			}
		}
	}
	names := []string{}
	for name := range candidates {
		if _, reported := vocabulary.Find(name); !reported {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
