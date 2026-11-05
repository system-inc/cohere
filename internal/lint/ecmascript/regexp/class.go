package regexp

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/unicodeproperties"
)

// classAtomKind says what one member of a character class is. Keeping the four
// apart is what lets a range be read as a range only where one was written: in
// `[\d-A]` the `-` separates no two characters, so JavaScript reads three
// members rather than a range, and the `A` goes on being a character that a
// case-insensitive class has to widen.
type classAtomKind uint8

const (
	// classRune is one character.
	classRune classAtomKind = iota
	// classRange is every character between two, inclusive.
	classRange
	// classSet is a set escape, which is written out as it stands and can
	// bound no range.
	classSet
	// classDash is a `-` that separates nothing and so stands for itself.
	classDash
)

type classAtom struct {
	kind   classAtomKind
	lo, hi rune
	text   string
	ranges []unicodeproperties.Range
}

// covers reports whether the atom already names `character`, which is what decides
// whether widening the class has anything to add for it.
func (atom classAtom) covers(character rune) bool {
	switch atom.kind {
	case classRune, classDash:
		return character == atom.lo
	case classRange:
		return character >= atom.lo && character <= atom.hi
	case classSet:
		_, ok := slices.BinarySearchFunc(atom.ranges, character, func(span unicodeproperties.Range, target rune) int {
			if span.Hi < target {
				return -1
			}
			if span.Lo > target {
				return 1
			}
			return 0
		})
		return ok
	}
	return false
}

func (atom classAtom) write() string {
	switch atom.kind {
	case classSet:
		return atom.text
	case classRange:
		return literalRune(atom.lo) + "-" + literalRune(atom.hi)
	default:
		return literalRune(atom.lo)
	}
}

// wordClassAtoms spells `\w` as class members, in ascending order. ECMAScript
// builds the set out of ASCII whatever the flags say, and under `u` and `i`
// together it gains the two characters that fold into it: U+017F LATIN SMALL
// LETTER LONG S onto `s`, U+212A KELVIN SIGN onto `k`.
func wordClassAtoms(options rewriteOptions) []classAtom {
	atoms := []classAtom{
		{kind: classRange, lo: '0', hi: '9'},
		{kind: classRange, lo: 'A', hi: 'Z'},
		{kind: classRune, lo: '_'},
		{kind: classRange, lo: 'a', hi: 'z'},
	}
	if options.ignoreCase && options.unicode {
		atoms = append(atoms,
			classAtom{kind: classRune, lo: 0x017F},
			classAtom{kind: classRune, lo: 0x212A},
		)
	}
	return atoms
}

// nonWordClassAtoms spells `\W` as class members. Out in a pattern a negated
// class says it in one word, but inside a class there is nothing to negate
// against, so the complement is written as the ranges between the word
// characters.
func nonWordClassAtoms(options rewriteOptions) []classAtom {
	span := func(lo, hi rune) classAtom {
		if lo == hi {
			return classAtom{kind: classRune, lo: lo}
		}
		return classAtom{kind: classRange, lo: lo, hi: hi}
	}

	atoms := []classAtom{}
	next := rune(0)
	for _, word := range wordClassAtoms(options) {
		hi := word.lo
		if word.kind == classRange {
			hi = word.hi
		}
		if next < word.lo {
			atoms = append(atoms, span(next, word.lo-1))
		}
		next = hi + 1
	}
	return append(atoms, span(next, unicode.MaxRune))
}

// readClass reads a character class off the front of source, which opens at
// `[`, returning its body, whether it is negated and how many bytes it spans.
func readClass(source string) (body string, negated bool, width int, readClassError error) {
	index := len("[")
	if index < len(source) && source[index] == '^' {
		negated = true
		index++
	}
	start := index
	for index < len(source) {
		switch source[index] {
		case '\\':
			_, size := utf8.DecodeRuneInString(source[index+1:])
			index += 1 + size
		case ']':
			return source[start:index], negated, index + 1, nil
		default:
			_, size := utf8.DecodeRuneInString(source[index:])
			index += size
		}
	}
	return "", false, 0, fmt.Errorf("%w: [ that no ] closes", ErrUnsupportedSyntax)
}

// classAtoms reads a class body into the members it names. The second result is
// false when an escape cannot be widened exactly.
func classAtoms(body string, options rewriteOptions, escaping escapeContext) ([]classAtom, bool, error) {
	escaping.inClass = true
	atoms := []classAtom{}
	exact := true

	for index := 0; index < len(body); {
		character, size := utf8.DecodeRuneInString(body[index:])
		if character != '\\' {
			if character == '-' {
				atoms = append(atoms, classAtom{kind: classDash, lo: '-'})
			} else {
				atoms = append(atoms, classAtom{kind: classRune, lo: character})
			}
			index += size
			continue
		}

		escape, decodeEscapeError := decodeEscape(body, index+size, escaping)
		if decodeEscapeError != nil {
			return nil, false, decodeEscapeError
		}
		end := index + size + escape.width
		switch escape.kind {
		case escapeSet:
			// regexp2 reads `\d` and `\s` the way ECMAScript does, and `\w`
			// only until `u` and `i` together widen the set past ASCII.
			switch {
			case escape.set == setProperty:
				atoms = append(atoms, propertyAtom(escape))
			case escape.set == setWord && options.ignoreCase && options.unicode:
				atoms = append(atoms, wordClassAtoms(options)...)
			case escape.set == setNonWord && options.ignoreCase && options.unicode:
				atoms = append(atoms, nonWordClassAtoms(options)...)
			default:
				atoms = append(atoms, classAtom{kind: classSet, text: body[index:end]})
			}
		default:
			atoms = append(atoms, classAtom{kind: classRune, lo: escape.r})
		}
		index = end
	}

	joined, joinRangesError := joinRanges(atoms, options)
	if joinRangesError != nil {
		return nil, false, joinRangesError
	}
	return joined, exact, nil
}

// joinRanges reads the `x-y` ranges out of a class's members. A `-` with a set
// on either side of it separates nothing, which Annex B reads as three members
// and `u` rejects outright.
func joinRanges(atoms []classAtom, options rewriteOptions) ([]classAtom, error) {
	joined := make([]classAtom, 0, len(atoms))
	for index := 0; index < len(atoms); index++ {
		if index+2 < len(atoms) && atoms[index+1].kind == classDash {
			lo, hi := atoms[index], atoms[index+2]
			if lo.kind == classRune && hi.kind == classRune {
				if lo.lo > hi.lo {
					return nil, fmt.Errorf("%w: a character range running backwards", ErrUnsupportedSyntax)
				}
				joined = append(joined, classAtom{kind: classRange, lo: lo.lo, hi: hi.lo})
				index += 2
				continue
			}
			if options.unicode {
				return nil, fmt.Errorf("%w: a set at the end of a character range", ErrUnsupportedSyntax)
			}
		}
		joined = append(joined, atoms[index])
	}
	return joined, nil
}

// writeClass writes the members out as a class regexp2 reads the same way,
// widening it first where the flags ask for it.
func writeClass(atoms []classAtom, negated bool, options rewriteOptions) string {
	if slices.ContainsFunc(atoms, func(atom classAtom) bool { return atom.kind == classSet && atom.ranges != nil }) {
		return writePropertyClass(atoms, negated, options)
	}
	if len(atoms) == 0 {
		// `[]` never matches and `[^]` matches anything: syntax only JavaScript
		// reads, and regexp2 would take the brackets literally.
		if negated {
			return anyCharacter
		}
		return neverMatches
	}

	var body strings.Builder
	for _, atom := range atoms {
		body.WriteString(atom.write())
	}
	if options.ignoreCase {
		body.WriteString(caseExtras(atoms, options.unicode))
	}
	if negated {
		return "[^" + body.String() + "]"
	}
	return "[" + body.String() + "]"
}

// caseExtras is what a class has to gain to cover every character a `/i`
// comparison accepts for one it already covers.
//
// What the class already names is left alone and the rest is appended, so a
// negated class goes on negating whatever the widened class covers. That is how
// JavaScript reads one too: `[^a]` asks whether any member compares equal to
// the character at hand, and answers no to `A`.
func caseExtras(atoms []classAtom, unicodeMode bool) string {
	covers := func(character rune) bool {
		return slices.ContainsFunc(atoms, func(atom classAtom) bool { return atom.covers(character) })
	}

	var extras strings.Builder
	for _, members := range CaseEquivalenceGroups(unicodeMode) {
		if !slices.ContainsFunc(members, covers) {
			continue
		}
		for _, member := range members {
			if !covers(member) {
				extras.WriteString(literalRune(member))
			}
		}
	}
	return extras.String()
}
