package regexp

import (
	"slices"
	"strings"
	"unicode"

	"github.com/system-inc/cohere/unicodeproperties"
)

// propertyAtom remains one set atom even though it contains many intervals. Expanding it into
// rune atoms before joinRanges would let a following dash turn its last interval into an endpoint.
func propertyAtom(escape decodedEscape) classAtom {
	property, _ := unicodeproperties.Lookup(escape.property)
	ranges := property.Ranges
	if escape.negated {
		// ECMAScript u complements before case closure. For example Greek mu is in Greek, but the
		// micro sign is not: both therefore match \P{Script=Greek} under iu after closure.
		complement := []unicodeproperties.Range{}
		next := rune(0)
		for _, r := range ranges {
			if next < r.Lo {
				complement = append(complement, unicodeproperties.Range{Lo: next, Hi: r.Lo - 1})
			}
			next = r.Hi + 1
		}
		if next <= unicode.MaxRune {
			complement = append(complement, unicodeproperties.Range{Lo: next, Hi: unicode.MaxRune})
		}
		ranges = complement
	}
	var text strings.Builder
	for _, r := range ranges {
		text.WriteString(literalRune(r.Lo))
		if r.Hi != r.Lo {
			text.WriteByte('-')
			text.WriteString(literalRune(r.Hi))
		}
	}
	return classAtom{kind: classSet, text: text.String(), ranges: ranges}
}

// writePropertyClass normalizes the entire union after closure, before emitting ranges. regexp2
// canonicalizes after every added range and can invert a nearly-full class prematurely; appending
// fold members after that inversion would change the excluded set instead of the included set.
func writePropertyClass(atoms []classAtom, negated bool, options rewriteOptions) string {
	var ranges []unicodeproperties.Range
	for _, a := range atoms {
		switch a.kind {
		case classRune, classDash:
			ranges = append(ranges, unicodeproperties.Range{Lo: a.lo, Hi: a.lo})
		case classRange:
			ranges = append(ranges, unicodeproperties.Range{Lo: a.lo, Hi: a.hi})
		case classSet:
			if a.ranges != nil {
				ranges = append(ranges, a.ranges...)
			} else {
				ranges = append(ranges, builtinSetRanges(a.text)...)
			}
		}
	}
	ranges = mergePropertyRanges(ranges)
	if options.ignoreCase {
		var extras []unicodeproperties.Range
		for _, group := range CaseEquivalenceGroups(options.unicode) {
			if slices.ContainsFunc(group, func(r rune) bool { return propertyRangesCover(ranges, r) }) {
				for _, r := range group {
					extras = append(extras, unicodeproperties.Range{Lo: r, Hi: r})
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
	for _, r := range ranges {
		out.WriteString(literalRune(r.Lo))
		if r.Lo != r.Hi {
			out.WriteByte('-')
			out.WriteString(literalRune(r.Hi))
		}
	}
	out.WriteByte(']')
	return out.String()
}
func propertyRangesCover(ranges []unicodeproperties.Range, r rune) bool {
	_, ok := slices.BinarySearchFunc(ranges, r, func(span unicodeproperties.Range, r rune) int {
		if span.Hi < r {
			return -1
		}
		if span.Lo > r {
			return 1
		}
		return 0
	})
	return ok
}
func mergePropertyRanges(ranges []unicodeproperties.Range) []unicodeproperties.Range {
	slices.SortFunc(ranges, func(a, b unicodeproperties.Range) int { return int(a.Lo - b.Lo) })
	out := ranges[:0]
	for _, r := range ranges {
		if len(out) > 0 && r.Lo <= out[len(out)-1].Hi+1 {
			out[len(out)-1].Hi = max(out[len(out)-1].Hi, r.Hi)
		} else {
			out = append(out, r)
		}
	}
	return out
}
func builtinSetRanges(text string) []unicodeproperties.Range {
	var ranges []unicodeproperties.Range
	switch text[1] {
	case 'd', 'D':
		ranges = []unicodeproperties.Range{{Lo: '0', Hi: '9'}}
	case 'w', 'W':
		ranges = []unicodeproperties.Range{{Lo: '0', Hi: '9'}, {Lo: 'A', Hi: 'Z'}, {Lo: '_', Hi: '_'}, {Lo: 'a', Hi: 'z'}}
	case 's', 'S':
		ranges = []unicodeproperties.Range{{Lo: 9, Hi: 13}, {Lo: 0x20, Hi: 0x20}, {Lo: 0xa0, Hi: 0xa0}, {Lo: 0x1680, Hi: 0x1680}, {Lo: 0x2000, Hi: 0x200a}, {Lo: 0x2028, Hi: 0x2029}, {Lo: 0x202f, Hi: 0x202f}, {Lo: 0x205f, Hi: 0x205f}, {Lo: 0x3000, Hi: 0x3000}, {Lo: 0xfeff, Hi: 0xfeff}}
	}
	if text[1] >= 'A' && text[1] <= 'Z' {
		complement := []unicodeproperties.Range{}
		next := rune(0)
		for _, r := range ranges {
			if next < r.Lo {
				complement = append(complement, unicodeproperties.Range{Lo: next, Hi: r.Lo - 1})
			}
			next = r.Hi + 1
		}
		if next <= unicode.MaxRune {
			complement = append(complement, unicodeproperties.Range{Lo: next, Hi: unicode.MaxRune})
		}
		return complement
	}
	return ranges
}
