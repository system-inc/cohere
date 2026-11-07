// Package folder_words is whether a folder's name runs words together, in one place, for every engine that judges
// folder names (#narw7aq, the text rules family `separated-folder-words`).
//
// The Go engine's cohere-go/consistency-require-separated-folder-words and the TypeScript rule
// nexus/consistency-require-separated-folder-words both read it, so the words, the pieces a split may use and how a
// tie is broken are decided once. Each engine hands it a name and its language's Spelling, and decides only its own
// language's facts first: which folders are its own to judge, and where a finding sits. It lives in its own package,
// importing nothing heavier than policy, because the nexus rules package links the TypeScript checker and the Go
// engine must not.
//
// A name is judged one part at a time, a part being a run between two of the spelling's separators, or between a
// lowercase letter and a capital where the spelling's case separates: a part that is a word passes, and a part that
// splits into two or more words is the finding, with the split as the spelling it asks for. The words are policy's
// (policy/Words.txt, cspell's lists with policy/HouseWords.txt merged in, and Brands.json's brands joined), so a word
// the lists lack is a reviewed change to policy, never a carve-out here.
package folder_words

import (
	"strings"
	"unicode"

	"github.com/system-inc/cohere/policy"
)

// Spelling is how one language's folder names separate their words.
type Spelling struct {
	// Separators are the characters that end one part of a name and start the next. Each is kept where the name
	// has it, so a reported name is changed only inside the part that runs words together.
	Separators string
	// Joiner joins the pieces of a split when the name separates no two parts already. Where it does, the split
	// takes the name's own first separator between two parts, so a name keeps one way of separating its words.
	Joiner string
	// CaseSeparates says a lowercase letter followed by an uppercase one ends a part, as a separator does, so
	// serviceTypes is service and Types and ProfileImages is Profile and Images, words a reader sees apart. Its
	// parts keep the name's case, and a split that is spelled by case is spelled by case too: Typesprofileimages
	// asks for TypesProfileImages. Go's package names hold no capitals, so its spelling leaves this false and reads a
	// name lowercased whole, as it always has.
	CaseSeparates bool
}

// minimumPieceLength is the fewest letters a piece of a split may have, unless it is one of twoLetterWords. It is what
// keeps a name the lists cannot read from splitting into garbage: the lists hold hundreds of two-letter entries,
// abbreviations and interjections among them, and with those as pieces acheck splits as ac_heck and withc as wi_thc.
// With the minimum, a name either splits into words a reader would name (cgo_test, cure_calls) or does not split,
// and passes.
const minimumPieceLength = 3

// twoLetterWords are the two-letter pieces a split may use despite minimumPieceLength: the ones folder names in Go
// projects run together with a word (go_engine, ts_path, next_js, io_test), each a name a reader knows on sight.
// The set is closed, so a two-letter piece is a reviewed decision here rather than whatever the lists hold. A
// member counts as a piece whether or not the lists hold it, since js and others are names, not dictionary words.
// Go has no constant set, so this is a variable nothing writes.
var twoLetterWords = map[string]bool{
	"cc": true,
	"db": true,
	"go": true,
	"io": true,
	"js": true,
	"os": true,
	"ts": true,
	"ui": true,
}

// brandWords are Brands.json's brands, lowercased and letters alone, so a brand a folder spells is a word as the
// lists' words are. The lists are cspell's, and a brand the house lists is the house's decision, not cspell's. Today
// the one brand, iMessage, is in cspell's software terms as well.
var brandWords = normalizedBrands(policy.Brands)

func normalizedBrands(vocabulary policy.BrandVocabulary) map[string]bool {
	words := make(map[string]bool, len(vocabulary.Brands))
	for _, entry := range vocabulary.Brands {
		words[strings.Map(func(character rune) rune {
			if character > unicode.MaxASCII || !unicode.IsLetter(character) {
				return -1
			}
			return unicode.ToLower(character)
		}, entry.Brand)] = true
	}
	return words
}

// Separated is name with each part that runs words together split into them, and whether any part did. A part is
// judged lowercased: a part that is a word is kept whole, and so is one that splits no way into pieces that are each
// a word, which is a name the lists cannot read (vfs, cachedvfs) rather than words run together. A word of three
// letters or more would come out of fewestPieces whole too, as its own one piece, so the word check first is the
// short way to the common answer, one search rather than one for every piece a split tries, and the answer for a word
// of one or two letters, which is no piece. A part that is a plural of a word is kept whole too (isPlural).
//
// Where the spelling's case separates, the name keeps its case, and a split part's pieces are joined by case when
// the part opens with a capital or the name separates its parts by case alone. Otherwise the name comes back
// lowercased, its split parts joined as the spelling's Joiner says.
func Separated(name string, spelling Spelling) (string, bool) {
	parts, separators := splitParts(name, spelling)
	joiner := spelling.Joiner
	separatedByCaseAlone := false
	for index, separator := range separators {
		if parts[index] == "" || parts[index+1] == "" {
			continue
		}
		if separator != "" {
			joiner = separator
			separatedByCaseAlone = false
			break
		}
		separatedByCaseAlone = true
	}
	runTogether := false
	for index, part := range parts {
		lowered := strings.ToLower(part)
		if !spelling.CaseSeparates {
			parts[index] = lowered
		}
		if lowered == "" || isWord(lowered) || isPlural(lowered) {
			continue
		}
		pieces := fewestPieces(lowered)
		if len(pieces) < 2 {
			continue
		}
		runTogether = true
		if spelling.CaseSeparates && (separatedByCaseAlone || startsUppercase(part)) {
			parts[index] = joinedByCase(pieces, startsUppercase(part))
			continue
		}
		parts[index] = strings.Join(pieces, joiner)
	}
	var joined strings.Builder
	for index, part := range parts {
		joined.WriteString(part)
		if index < len(separators) {
			joined.WriteString(separators[index])
		}
	}
	return joined.String(), runTogether
}

// splitParts is name cut at each of the spelling's separators, and where its case separates, between a lowercase
// letter and the uppercase one after it: the parts and, between each two, the separator that stood there, empty for
// a case boundary. A part is empty where two separators meet or one leads or ends the name.
func splitParts(name string, spelling Spelling) ([]string, []string) {
	parts := []string{}
	between := []string{}
	start := 0
	for index := 0; index < len(name); index++ {
		if strings.IndexByte(spelling.Separators, name[index]) >= 0 {
			parts = append(parts, name[start:index])
			between = append(between, name[index:index+1])
			start = index + 1
			continue
		}
		if spelling.CaseSeparates && index > start && isLowercase(name[index-1]) && isUppercase(name[index]) {
			parts = append(parts, name[start:index])
			between = append(between, "")
			start = index
		}
	}
	return append(parts, name[start:]), between
}

// joinedByCase is pieces spelled as one camel-cased run: each after the first opens with a capital, and the first
// does when capitalized says so.
func joinedByCase(pieces []string, capitalized bool) string {
	var joined strings.Builder
	for index, piece := range pieces {
		if index > 0 || capitalized {
			joined.WriteString(strings.ToUpper(piece[:1]) + piece[1:])
			continue
		}
		joined.WriteString(piece)
	}
	return joined.String()
}

// startsUppercase reports whether text opens with an ASCII capital.
func startsUppercase(text string) bool {
	return text != "" && isUppercase(text[0])
}

func isLowercase(character byte) bool {
	return 'a' <= character && character <= 'z'
}

func isUppercase(character byte) bool {
	return 'A' <= character && character <= 'Z'
}

// isWord reports whether text is in the lists or is a brand, at any length.
func isWord(text string) bool {
	return brandWords[text] || policy.KnownWords().Has(text)
}

// isPlural reports whether text is a word with `s` or `es` after it. cspell's en_US holds the inflections its source
// lists, and its source gives some words no plural: it holds automation and not automations, webhook and not
// webhooks (#narw7aq). A folder named for what it holds is most often a plural, so without this a plural the lists
// lack splits into whatever pieces it can, automat_ions. Only a whole part is read this way, never a piece of a
// split, so a name that runs words together, jsonlines, still splits as it did, and the suffix is never a word of
// its own.
func isPlural(text string) bool {
	if stem, cut := strings.CutSuffix(text, "es"); cut && isWord(stem) {
		return true
	}
	stem, cut := strings.CutSuffix(text, "s")
	return cut && isWord(stem)
}

// isPiece reports whether text may be one piece of a split: one of twoLetterWords, or a word of minimumPieceLength
// letters or more.
func isPiece(text string) bool {
	return twoLetterWords[text] || len(text) >= minimumPieceLength && isWord(text)
}

// fewestPieces is part split into the fewest pieces, or nil when no split exists.
//
// Two splits can tie: runoutput is run_output or runout_put, and gatesample gate_sample or gates_ample. The tie goes
// to the split whose first piece is shortest, then its second, and so on. A longer first piece is most often the
// earlier word swallowing the start of the next as an inflection or a particle (gates, runout), and the shorter one
// leaves the next word whole. The rule is a fixed order over the pieces, so a name splits the same way on every run.
//
// pieceCount[start] is the fewest pieces part[start:] splits into, or 0 when it splits no way, and pieceEnd[start] is
// where the first of them ends. Filling them from the end, each start tries its pieces shortest first and keeps only
// a strictly better count, so the shortest first piece of a fewest-pieces split is the one kept.
func fewestPieces(part string) []string {
	pieceCount := make([]int, len(part)+1)
	pieceEnd := make([]int, len(part)+1)
	for start := len(part) - 1; start >= 0; start-- {
		for end := start + 1; end <= len(part); end++ {
			rest := 0
			if end < len(part) {
				rest = pieceCount[end]
				if rest == 0 {
					continue
				}
			}
			if (pieceCount[start] == 0 || rest+1 < pieceCount[start]) && isPiece(part[start:end]) {
				pieceCount[start] = rest + 1
				pieceEnd[start] = end
			}
		}
	}
	if pieceCount[0] == 0 {
		return nil
	}
	pieces := make([]string, 0, pieceCount[0])
	for start := 0; start < len(part); start = pieceEnd[start] {
		pieces = append(pieces, part[start:pieceEnd[start]])
	}
	return pieces
}
