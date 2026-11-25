package regexp

import (
	"slices"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/unicode_properties"
)

// propertyAtom remains one set atom even though it contains many intervals. Expanding it into
// rune atoms before joinRanges would let a following dash turn its last interval into an endpoint.
func propertyAtom(escape decodedEscape) classAtom {
	property, _ := unicode_properties.Lookup(escape.property)
	ranges := property.Ranges
	if escape.negated {
		// ECMAScript u complements before case closure. For example Greek mu is in Greek, but the
		// micro sign is not: both therefore match \P{Script=Greek} under iu after closure.
		complement := []unicode_properties.Range{}
		next := rune(0)
		for _, span := range ranges {
			if next < span.Lo {
				complement = append(complement, unicode_properties.Range{Lo: next, Hi: span.Lo - 1})
			}
			next = span.Hi + 1
		}
		if next <= unicode.MaxRune {
			complement = append(complement, unicode_properties.Range{Lo: next, Hi: unicode.MaxRune})
		}
		ranges = complement
	}
	var text strings.Builder
	for _, span := range ranges {
		text.WriteString(literalRune(span.Lo))
		if span.Hi != span.Lo {
			text.WriteByte('-')
			text.WriteString(literalRune(span.Hi))
		}
	}
	return classAtom{kind: classSet, text: text.String(), ranges: ranges}
}

// writePropertyClass normalizes the entire union after closure, before emitting ranges. regexp2
// canonicalizes after every added range and can invert a nearly-full class prematurely; appending
// fold members after that inversion would change the excluded set instead of the included set.
func writePropertyClass(atoms []classAtom, negated bool, options rewriteOptions) string {
	var ranges []unicode_properties.Range
	for _, atom := range atoms {
		switch atom.kind {
		case classRune, classDash:
			ranges = append(ranges, unicode_properties.Range{Lo: atom.lo, Hi: atom.lo})
		case classRange:
			ranges = append(ranges, unicode_properties.Range{Lo: atom.lo, Hi: atom.hi})
		case classSet:
			if atom.ranges != nil {
				ranges = append(ranges, atom.ranges...)
			} else {
				ranges = append(ranges, builtinSetRanges(atom.text)...)
			}
		}
	}
	ranges = mergePropertyRanges(ranges)
	if options.ignoreCase {
		var extras []unicode_properties.Range
		for _, group := range CaseEquivalenceGroups(options.unicode) {
			if slices.ContainsFunc(group, func(character rune) bool { return propertyRangesCover(ranges, character) }) {
				for _, character := range group {
					extras = append(extras, unicode_properties.Range{Lo: character, Hi: character})
				}
			}
		}
		ranges = mergePropertyRanges(append(ranges, extras...))
	}
	if len(ranges) == 0 {
		if negated {
			return anyCharacter
		}
		return neverMatches
	}
	var out strings.Builder
	out.WriteByte('[')
	if negated {
		out.WriteByte('^')
	}
	for _, span := range ranges {
		out.WriteString(literalRune(span.Lo))
		if span.Lo != span.Hi {
			out.WriteByte('-')
			out.WriteString(literalRune(span.Hi))
		}
	}
	out.WriteByte(']')
	return out.String()
}
func propertyRangesCover(ranges []unicode_properties.Range, character rune) bool {
	_, ok := slices.BinarySearchFunc(ranges, character, func(span unicode_properties.Range, target rune) int {
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
func mergePropertyRanges(ranges []unicode_properties.Range) []unicode_properties.Range {
	slices.SortFunc(ranges, func(left, right unicode_properties.Range) int { return int(left.Lo - right.Lo) })
	out := ranges[:0]
	for _, span := range ranges {
		if len(out) > 0 && span.Lo <= out[len(out)-1].Hi+1 {
			out[len(out)-1].Hi = max(out[len(out)-1].Hi, span.Hi)
		} else {
			out = append(out, span)
		}
	}
	return out
}
func builtinSetRanges(text string) []unicode_properties.Range {
	var ranges []unicode_properties.Range
	switch text[1] {
	case 'd', 'D':
		ranges = []unicode_properties.Range{{Lo: '0', Hi: '9'}}
	case 'w', 'W':
		ranges = []unicode_properties.Range{
			{Lo: '0', Hi: '9'},
			{Lo: 'A', Hi: 'Z'},
			{Lo: '_', Hi: '_'},
			{Lo: 'a', Hi: 'z'},
		}
	case 's', 'S':
		ranges = []unicode_properties.Range{
			{Lo: 9, Hi: 13},
			{Lo: 0x20, Hi: 0x20},
			{Lo: 0xa0, Hi: 0xa0},
			{Lo: 0x1680, Hi: 0x1680},
			{Lo: 0x2000, Hi: 0x200a},
			{Lo: 0x2028, Hi: 0x2029},
			{Lo: 0x202f, Hi: 0x202f},
			{Lo: 0x205f, Hi: 0x205f},
			{Lo: 0x3000, Hi: 0x3000},
			{Lo: 0xfeff, Hi: 0xfeff},
		}
	}
	if text[1] >= 'A' && text[1] <= 'Z' {
		complement := []unicode_properties.Range{}
		next := rune(0)
		for _, span := range ranges {
			if next < span.Lo {
				complement = append(complement, unicode_properties.Range{Lo: next, Hi: span.Lo - 1})
			}
			next = span.Hi + 1
		}
		if next <= unicode.MaxRune {
			complement = append(complement, unicode_properties.Range{Lo: next, Hi: unicode.MaxRune})
		}
		return complement
	}
	return ranges
}
