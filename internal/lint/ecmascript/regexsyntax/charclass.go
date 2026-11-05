// cspell:ignore octals
package regexsyntax

import (
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// RegexFlags captures the subset of ECMAScript regex flags that affects how
// patterns (and in particular character classes) are parsed.
type RegexFlags struct {
	Unicode     bool // u flag
	UnicodeSets bool // v flag
}

// UnicodeMode reports whether unicode/unicodeSets mode is active (either u or v).
func (flags RegexFlags) UnicodeMode() bool { return flags.Unicode || flags.UnicodeSets }

// ParseRegexFlags returns a RegexFlags from a flag string (e.g. "gui").
func ParseRegexFlags(flags string) RegexFlags {
	return RegexFlags{
		Unicode:     strings.ContainsRune(flags, 'u'),
		UnicodeSets: strings.ContainsRune(flags, 'v'),
	}
}

// RegexCharElementKind classifies one element of a character class body.
type RegexCharElementKind int

const (
	// RegexCharSingle is a single character element (literal, escape, …).
	RegexCharSingle RegexCharElementKind = iota
	// RegexCharRange is a character range `a-b` (inclusive on both ends).
	// Value is the `a` side; Max is the `b` side. Only the min/max endpoints
	// appear in the resulting character sequence: the inner characters are
	// implicit.
	RegexCharRange
	// RegexCharBreaker is any element that interrupts a contiguous character
	// sequence within a class: `\d`, `\D`, `\w`, `\W`, `\s`, `\S`, `\b`, `\B`,
	// `\p{...}`, `\P{...}`, `\q{...}` (v-flag), nested `[...]` (v-flag), or a
	// v-flag set operator `--` / `&&`.
	RegexCharBreaker
)

// RegexCharElement is one element of a parsed character class body.
//
// For RegexCharSingle:
//   - Value holds the element's effective value (UTF-16 code unit or the
//     astral code point when combined via `\uHHHH\uHHHH` under u/v).
//   - IsUBrace is true iff the element was written as `\u{H...}`.
//   - IsLoneSurrogate is true iff the element is a lone surrogate from a raw
//     astral character under non-u mode (where regexpp sees it as two units).
//
// For RegexCharRange:
//   - Value / IsUBrace describe the `a` endpoint.
//   - Max / MaxIsUBrace describe the `b` endpoint.
//
// For RegexCharBreaker:
//   - Value, Max, IsUBrace are unused.
//
// Start / End are byte offsets within the pattern text covering the element's
// source extent.
type RegexCharElement struct {
	Kind            RegexCharElementKind
	Value           uint32
	IsUBrace        bool
	IsLoneSurrogate bool

	Max         uint32
	MaxIsUBrace bool

	Start int
	End   int

	// RawStart/RawEnd optionally narrow to just the element's own source
	// extent, distinct from Start/End which always span the element. For
	// ranges this pair equals Start/End.
}

// ---------------------------------------------------------------------------
// Pattern scanner (layer 1)
// ---------------------------------------------------------------------------

// IterateRegexCharacterClasses walks a regex pattern and invokes cb once per
// top-level character class (including any v-flag nested classes, at each
// nesting level). cb receives the byte range [start, end) covering `[`..`]`.
//
// Returns false if the pattern is malformed (unterminated class, unterminated
// escape at EOF). When false, cb may have been invoked for classes encountered
// before the error.
func IterateRegexCharacterClasses(pattern string, flags RegexFlags, cb func(start, end int)) bool {
	index := 0
	for index < len(pattern) {
		switch pattern[index] {
		case '\\':
			step, ok := SkipPatternEscape(pattern, index, flags)
			if !ok {
				return false
			}
			index += step
		case '[':
			end, ok := iterateClassFromLBracket(pattern, index, flags, cb)
			if !ok {
				return false
			}
			index = end
		case '(':
			// Parens don't affect character-class scanning; consume and continue.
			index++
		default:
			_, size := utf8.DecodeRuneInString(pattern[index:])
			if size == 0 {
				index++
			} else {
				index += size
			}
		}
	}
	return true
}

// iterateClassFromLBracket recursively scans a character class starting at
// pattern[start] (which must be `[`). Invokes cb for the class (and, under v
// flag, for any nested classes). Returns the index just past `]`, or ok=false
// on malformed input.
func iterateClassFromLBracket(pattern string, start int, flags RegexFlags, cb func(start, end int)) (int, bool) {
	end, ok := ClassEnd(pattern, start, flags)
	if !ok {
		return start, false
	}
	// Recurse into nested classes first (under v flag), then invoke cb on
	// the outermost.
	if flags.UnicodeSets {
		index := start + 1
		if index < end-1 && pattern[index] == '^' {
			index++
		}
		for index < end-1 {
			character := pattern[index]
			if character == '\\' {
				step, ok := SkipPatternEscape(pattern, index, flags)
				if !ok {
					return start, false
				}
				index += step
				continue
			}
			if character == '[' {
				nestedEnd, ok := iterateClassFromLBracket(pattern, index, flags, cb)
				if !ok {
					return start, false
				}
				index = nestedEnd
				continue
			}
			_, size := utf8.DecodeRuneInString(pattern[index:])
			if size == 0 {
				index++
			} else {
				index += size
			}
		}
	}
	cb(start, end)
	return end, true
}

// ClassEnd returns the byte index just past the matching `]` for a class
// starting at `[` at pattern[start]. Handles escaped `]`, v-flag nested
// classes, and `\q{...}` which contains a literal `]` inside braces.
func ClassEnd(pattern string, start int, flags RegexFlags) (int, bool) {
	if start >= len(pattern) || pattern[start] != '[' {
		return start, false
	}
	index := start + 1
	// A leading `^` is not an element by itself.
	if index < len(pattern) && pattern[index] == '^' {
		index++
	}
	depth := 1
	for index < len(pattern) {
		character := pattern[index]
		switch {
		case character == '\\':
			step, ok := SkipPatternEscape(pattern, index, flags)
			if !ok {
				return start, false
			}
			index += step
		case character == '[' && flags.UnicodeSets:
			depth++
			index++
		case character == ']':
			depth--
			index++
			if depth == 0 {
				return index, true
			}
		default:
			_, size := utf8.DecodeRuneInString(pattern[index:])
			if size == 0 {
				index++
			} else {
				index += size
			}
		}
	}
	return start, false
}

// SkipPatternEscape returns how many bytes a `\`-prefixed escape consumes at
// pattern[index] (including the leading `\`) for the purposes of class-boundary
// scanning. Returns ok=false at EOF on `\`.
func SkipPatternEscape(pattern string, index int, flags RegexFlags) (int, bool) {
	if index+1 >= len(pattern) {
		return 0, false
	}
	next := pattern[index+1]
	switch next {
	case 'x':
		if index+3 < len(pattern) && IsHexDigit(pattern[index+2]) && IsHexDigit(pattern[index+3]) {
			return 4, true
		}
		return 2, true
	case 'u':
		if index+2 < len(pattern) && pattern[index+2] == '{' {
			if !flags.UnicodeMode() {
				return 2, true
			}
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel < 0 {
				return 2, true // best-effort recover
			}
			return 3 + closeRel + 1, true
		}
		if index+5 < len(pattern) && AllHexDigits(pattern[index+2:index+6]) {
			return 6, true
		}
		return 2, true
	case 'c':
		if index+2 < len(pattern) {
			return 3, true
		}
		return 2, true
	case 'p', 'P':
		if flags.UnicodeMode() && index+2 < len(pattern) && pattern[index+2] == '{' {
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel >= 0 {
				return 3 + closeRel + 1, true
			}
		}
		return 2, true
	case 'q':
		if flags.UnicodeMode() && index+2 < len(pattern) && pattern[index+2] == '{' {
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel >= 0 {
				return 3 + closeRel + 1, true
			}
		}
		return 2, true
	}
	// Generic two-byte escape (covers identity, \d, \w, ...).
	// If the next is a multi-byte UTF-8 rune, consume its full width.
	_, size := utf8.DecodeRuneInString(pattern[index+1:])
	if size == 0 {
		return 2, true
	}
	return 1 + size, true
}

// ---------------------------------------------------------------------------
// Character class body parser (layer 2)
// ---------------------------------------------------------------------------

// ParseRegexCharacterClass parses a character class starting at pattern[start]
// (which must be `[`). It returns the flat element list (ranges expanded into
// min/max; nested classes and set operators emitted as breakers at the
// position where they appear) and the byte index just past the closing `]`.
//
// The input range [start, end) is trusted to be well-formed (e.g. from a
// prior call to IterateRegexCharacterClasses).
//
// Note: nested v-flag classes themselves are not recursed into by this
// function: they appear only as RegexCharBreaker. Callers that want to scan
// nested classes should iterate them via IterateRegexCharacterClasses and
// call ParseRegexCharacterClass on each separately.
func ParseRegexCharacterClass(pattern string, start int, flags RegexFlags) ([]RegexCharElement, int, bool) {
	return parseRegexCharacterClass(pattern, start, flags, 0)
}

const maxRegexCharacterClassPreallocation = 16 << 10

// ParseRegexCharacterClassWithEnd is ParseRegexCharacterClass with a
// previously discovered class end. Non-v callers use the known byte span as
// an element-capacity hint to avoid repeated slice growth for large classes.
// Unicode-set classes deliberately skip that preallocation:
// each nested callback receives the outer span, whose byte length can be much
// larger than its own flat element count. The cap also bounds over-allocation
// for escape-heavy and multibyte input, where bytes can outnumber elements.
func ParseRegexCharacterClassWithEnd(pattern string, start, end int, flags RegexFlags) ([]RegexCharElement, int, bool) {
	if start < 0 || end <= start || end > len(pattern) || pattern[end-1] != ']' {
		return nil, start, false
	}
	elementCapacity := 0
	if !flags.UnicodeSets {
		elementCapacity = end - start - 2
		if elementCapacity < 0 {
			elementCapacity = 0
		} else if elementCapacity > maxRegexCharacterClassPreallocation {
			elementCapacity = maxRegexCharacterClassPreallocation
		}
	}
	elements, parsedEnd, ok := parseRegexCharacterClass(pattern, start, flags, elementCapacity)
	if !ok || parsedEnd != end {
		return nil, start, false
	}
	if len(elements) == 0 {
		elements = nil
	}
	return elements, parsedEnd, true
}

func parseRegexCharacterClass(
	pattern string,
	start int,
	flags RegexFlags,
	elementCapacity int,
) ([]RegexCharElement, int, bool) {
	if start >= len(pattern) || pattern[start] != '[' {
		return nil, start, false
	}
	index := start + 1
	if index < len(pattern) && pattern[index] == '^' {
		index++
	}

	var out []RegexCharElement
	if elementCapacity > 0 {
		out = make([]RegexCharElement, 0, elementCapacity)
	}
	pendingIndex := -1 // index into `out` of the last RegexCharSingle (candidate for range start)

	for index < len(pattern) {
		character := pattern[index]
		switch {
		case character == ']':
			return out, index + 1, true

		case character == '\\':
			el, step, ok := readClassEscape(pattern, index, flags)
			if !ok {
				return nil, start, false
			}
			if el.Kind == RegexCharBreaker {
				out = append(out, el)
				pendingIndex = -1
			} else {
				// RegexCharSingle (possibly with companion astral surrogate entry below)
				out = append(out, el)
				pendingIndex = len(out) - 1
				// Under non-u/v mode, a raw astral character or a `\u{H}` with H>0xFFFF
				// should emit two units. But class escapes only produce 1 unit per call;
				// astral raw chars are handled in the `default` case below, and `\u{H}`
				// under u/v produces the combined code point (one element). Under non-u,
				// `\u{H}` is not recognized at all (treated as identity `u`).
			}
			index += step

		case character == '[' && flags.UnicodeSets:
			// v-flag nested class: emit as breaker; caller recurses via
			// IterateRegexCharacterClasses to parse nested contents.
			nestedEnd, ok := ClassEnd(pattern, index, flags)
			if !ok {
				return nil, start, false
			}
			out = append(out, RegexCharElement{Kind: RegexCharBreaker, Start: index, End: nestedEnd})
			index = nestedEnd
			pendingIndex = -1

		case (character == '-' || character == '&') &&
			flags.UnicodeSets &&
			index+1 < len(pattern) &&
			pattern[index+1] == character:
			// v-flag set operator `--` or `&&`: breaker
			out = append(out, RegexCharElement{Kind: RegexCharBreaker, Start: index, End: index + 2})
			index += 2
			pendingIndex = -1

		case character == '-' && pendingIndex >= 0 && index+1 < len(pattern) && pattern[index+1] != ']':
			// Range: combine pending with next element.
			index++
			var nextEl RegexCharElement
			var step int
			var ok bool
			if pattern[index] == '\\' {
				nextEl, step, ok = readClassEscape(pattern, index, flags)
				if !ok {
					return nil, start, false
				}
			} else {
				nextEl, step, ok = readRawClassChar(pattern, index, flags)
				if !ok {
					return nil, start, false
				}
			}
			if nextEl.Kind != RegexCharSingle {
				// Under u flag, `set-to-X` or `X-to-set` is a syntax error;
				// ESLint's regexpp would throw. We treat this as malformed
				// and abort class parsing (caller will skip the regex).
				return nil, start, false
			}
			minimumEl := out[pendingIndex]
			rangeEl := RegexCharElement{
				Kind:        RegexCharRange,
				Value:       minimumEl.Value,
				IsUBrace:    minimumEl.IsUBrace,
				Max:         nextEl.Value,
				MaxIsUBrace: nextEl.IsUBrace,
				Start:       minimumEl.Start,
				End:         nextEl.End,
			}
			out[pendingIndex] = rangeEl
			// If the min endpoint came from a raw astral in non-u mode
			// (which would logically expand to a surrogate pair), the range
			// representation is already degenerate; callers in this project
			// only inspect endpoints, so we don't split here.
			index += step
			pendingIndex = -1

		default:
			el, step, ok := readRawClassChar(pattern, index, flags)
			if !ok {
				return nil, start, false
			}
			if el.Kind == RegexCharSingle {
				out = append(out, el)
				pendingIndex = len(out) - 1
				// Note: we do not split raw astrals into surrogate pairs here.
				// Callers (e.g. the misleading-character-class rule) decide
				// per-flag whether to expand them, based on the rule's own
				// semantic needs. See IsLoneSurrogate on the element.
			}
			index += step
		}
	}
	return nil, start, false // unterminated
}

// readClassEscape parses a `\`-prefixed token inside a character class. It
// emits one RegexCharElement: either RegexCharSingle or RegexCharBreaker.
func readClassEscape(pattern string, index int, flags RegexFlags) (RegexCharElement, int, bool) {
	if index+1 >= len(pattern) {
		return RegexCharElement{}, 0, false
	}
	next := pattern[index+1]
	switch next {
	case 'd', 'D', 'w', 'W', 's', 'S', 'b', 'B':
		// Note: inside a class, `\b` means U+0008 (backspace), not word
		// boundary. ESLint treats it as a single character. `\B` inside a
		// class is a syntax error under u mode; under non-u it's identity.
		// For our purposes, we treat `\b` as a RegexCharSingle with value 8
		// (backspace) and `\B` ... hmm.
		//
		// Actually: regexpp treats `\b` in class as Character(0x08) and `\B`
		// in class as a syntax error in u mode but identity in non-u.
		// `\d`/`\D`/`\w`/`\W`/`\s`/`\S` are CharacterSet (breaker).
		if next == 'b' {
			return RegexCharElement{
				Kind: RegexCharSingle, Value: 0x08, Start: index, End: index + 2,
			}, 2, true
		}
		if next == 'B' {
			if flags.UnicodeMode() {
				return RegexCharElement{}, 0, false // u mode syntax error
			}
			// Under non-u, identity escape.
			return RegexCharElement{
				Kind: RegexCharSingle, Value: 'B', Start: index, End: index + 2,
			}, 2, true
		}
		return RegexCharElement{Kind: RegexCharBreaker, Start: index, End: index + 2}, 2, true
	case 'p', 'P':
		if flags.UnicodeMode() && index+2 < len(pattern) && pattern[index+2] == '{' {
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel < 0 {
				return RegexCharElement{}, 0, false
			}
			end := index + 3 + closeRel + 1
			return RegexCharElement{Kind: RegexCharBreaker, Start: index, End: end}, end - index, true
		}
		// Non-u mode: identity escape of `p`/`P`.
		return RegexCharElement{
			Kind: RegexCharSingle, Value: uint32(next), Start: index, End: index + 2,
		}, 2, true
	case 'q':
		if flags.UnicodeMode() && index+2 < len(pattern) && pattern[index+2] == '{' {
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel < 0 {
				return RegexCharElement{}, 0, false
			}
			end := index + 3 + closeRel + 1
			return RegexCharElement{Kind: RegexCharBreaker, Start: index, End: end}, end - index, true
		}
		return RegexCharElement{
			Kind: RegexCharSingle, Value: 'q', Start: index, End: index + 2,
		}, 2, true
	case 'x':
		if index+3 < len(pattern) && IsHexDigit(pattern[index+2]) && IsHexDigit(pattern[index+3]) {
			value := ParseHexUint(pattern[index+2 : index+4])
			return RegexCharElement{
				Kind: RegexCharSingle, Value: value, Start: index, End: index + 4,
			}, 4, true
		}
		return RegexCharElement{
			Kind: RegexCharSingle, Value: 'x', Start: index, End: index + 2,
		}, 2, true
	case 'u':
		if index+2 < len(pattern) && pattern[index+2] == '{' {
			if !flags.UnicodeMode() {
				// Non-u mode: treat `\u` as identity `u`.
				return RegexCharElement{
					Kind: RegexCharSingle, Value: 'u', Start: index, End: index + 2,
				}, 2, true
			}
			closeRel := strings.IndexByte(pattern[index+3:], '}')
			if closeRel < 0 {
				return RegexCharElement{}, 0, false
			}
			hex := pattern[index+3 : index+3+closeRel]
			end := index + 3 + closeRel + 1
			if hex == "" || !AllHexDigits(hex) {
				return RegexCharElement{}, 0, false
			}
			return RegexCharElement{
				Kind: RegexCharSingle, Value: ParseHexUint(hex), IsUBrace: true,
				Start: index, End: end,
			}, end - index, true
		}
		if index+5 < len(pattern) && AllHexDigits(pattern[index+2:index+6]) {
			hi := ParseHexUint(pattern[index+2 : index+6])
			// Surrogate pair `\uHHHH\uHHHH` under u/v collapses to one element
			// with an astral value.
			if flags.UnicodeMode() && hi >= 0xD800 && hi <= 0xDBFF && index+11 < len(pattern) &&
				pattern[index+6] == '\\' && pattern[index+7] == 'u' && AllHexDigits(pattern[index+8:index+12]) {
				lo := ParseHexUint(pattern[index+8 : index+12])
				if lo >= 0xDC00 && lo <= 0xDFFF {
					cp := 0x10000 + (hi-0xD800)*0x400 + (lo - 0xDC00)
					return RegexCharElement{
						Kind: RegexCharSingle, Value: cp, Start: index, End: index + 12,
					}, 12, true
				}
			}
			return RegexCharElement{
				Kind: RegexCharSingle, Value: hi, Start: index, End: index + 6,
			}, 6, true
		}
		return RegexCharElement{
			Kind: RegexCharSingle, Value: 'u', Start: index, End: index + 2,
		}, 2, true
	case 'n':
		return RegexCharElement{Kind: RegexCharSingle, Value: '\n', Start: index, End: index + 2}, 2, true
	case 't':
		return RegexCharElement{Kind: RegexCharSingle, Value: '\t', Start: index, End: index + 2}, 2, true
	case 'r':
		return RegexCharElement{Kind: RegexCharSingle, Value: '\r', Start: index, End: index + 2}, 2, true
	case 'v':
		return RegexCharElement{Kind: RegexCharSingle, Value: '\v', Start: index, End: index + 2}, 2, true
	case 'f':
		return RegexCharElement{Kind: RegexCharSingle, Value: '\f', Start: index, End: index + 2}, 2, true
	case '0':
		// In u/v mode `\0` is always NUL (no octal extension). In non-u
		// mode, if followed by a digit it's a legacy octal, but legacy
		// octals aren't meaningful for this project's detectors, so treat
		// as NUL for simplicity.
		return RegexCharElement{Kind: RegexCharSingle, Value: 0, Start: index, End: index + 2}, 2, true
	case 'c':
		if index+2 < len(pattern) {
			return RegexCharElement{
				Kind: RegexCharSingle, Value: uint32(pattern[index+2]) & 0x1F,
				Start: index, End: index + 3,
			}, 3, true
		}
		return RegexCharElement{}, 0, false
	}
	// Identity escape: value is the (possibly multi-byte) character after `\`.
	character, size := utf8.DecodeRuneInString(pattern[index+1:])
	if size == 0 {
		return RegexCharElement{}, 0, false
	}
	// Under u/v mode, identity escape of a letter/digit is a syntax error.
	// We relax this: let caller see the element and skip the regex if it
	// becomes problematic. (Most production code won't hit this.)
	return RegexCharElement{
		Kind: RegexCharSingle, Value: uint32(character), Start: index, End: index + 1 + size,
	}, 1 + size, true
}

// readRawClassChar reads a non-`\`-prefixed character inside a class body
// (i.e. a raw character at pattern[index]). Raw astral in non-u/v mode emits
// the high surrogate only: callers handle the low-surrogate companion by
// inspecting IsLoneSurrogate and stepping via a subsequent call.
//
// To keep the protocol simple, ParseRegexCharacterClass calls this once and
// trusts the single element returned; we do not split raw astrals here. The
// misleading-character-class rule accepts this because it gets raw astrals
// only in string-literal contexts, where the layer-3 parser already does the
// UTF-16 surrogate split.
func readRawClassChar(pattern string, index int, flags RegexFlags) (RegexCharElement, int, bool) {
	character, size := utf8.DecodeRuneInString(pattern[index:])
	if size == 0 {
		return RegexCharElement{}, 0, false
	}
	return RegexCharElement{
		Kind: RegexCharSingle, Value: uint32(character), Start: index, End: index + size,
	}, size, true
}
