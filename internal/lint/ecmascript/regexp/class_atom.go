package regexp

import (
	"slices"

	"github.com/system-inc/cohere/unicode_properties"
)

type classAtom struct {
	kind   classAtomKind
	lo, hi rune
	text   string
	ranges []unicode_properties.Range
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
		_, ok := slices.BinarySearchFunc(atom.ranges, character, func(span unicode_properties.Range, target rune) int {
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
