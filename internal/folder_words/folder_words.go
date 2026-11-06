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
// A name is judged one part at a time, a part being a run between two of the spelling's separators: a part that is a
// word passes, and a part that splits into two or more words is the finding, with the split as the spelling it asks
// for. The words are policy's (policy/Words.txt, cspell's lists with policy/HouseWords.txt merged in, and Brands.json's
// brands joined), so a word the lists lack is a reviewed change to policy, never a carve-out here.
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

// Separated is name lowercased with each part that runs words together split into them, and whether any part did. A
// part that is a word is kept whole, and so is one that splits no way into pieces that are each a word, which is a
// name the lists cannot read (vfs, cachedvfs) rather than words run together. A word of three letters or more would
// come out of fewestPieces whole too, as its own one piece, so the word check first is the short way to the common
// answer, one search rather than one for every piece a split tries, and the answer for a word of one or two letters,
// which is no piece.
func Separated(name string, spelling Spelling) (string, bool) {
	parts, separators := splitParts(strings.ToLower(name), spelling.Separators)
	joiner := spelling.Joiner
	for index, separator := range separators {
		if parts[index] != "" && parts[index+1] != "" {
			joiner = separator
			break
		}
	}
	runTogether := false
	for index, part := range parts {
		if part == "" || isWord(part) {
			continue
		}
		if pieces := fewestPieces(part); len(pieces) >= 2 {
			parts[index] = strings.Join(pieces, joiner)
			runTogether = true
		}
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

// splitParts is name cut at each of separators, the parts and, between each two, the separator that stood there, so
// a part is empty where two separators meet or one leads or ends the name.
func splitParts(name string, separators string) ([]string, []string) {
	parts := []string{}
	between := []string{}
	start := 0
	for index := 0; index < len(name); index++ {
		if strings.IndexByte(separators, name[index]) >= 0 {
			parts = append(parts, name[start:index])
			between = append(between, name[index:index+1])
			start = index + 1
		}
	}
	return append(parts, name[start:]), between
}

// isWord reports whether text is in the lists or is a brand, at any length.
func isWord(text string) bool {
	return brandWords[text] || policy.KnownWords().Has(text)
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
