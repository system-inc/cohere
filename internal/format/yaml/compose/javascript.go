package compose

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// JavaScript's string operations over UTF-16 code units, and the conversion between a JavaScript string
// and the Go string this package hands out. See the package comment, "Strings".

// unitsToString is a JavaScript string as a Go string: UTF-8 for every code point, and a lone surrogate,
// which UTF-8 cannot hold, in the three bytes its code point would take (WTF-8), so nothing is lost.
func unitsToString(units []uint16) string {
	var builder strings.Builder
	builder.Grow(len(units))
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit < utf8.RuneSelf:
			builder.WriteByte(byte(unit))
		case isHighSurrogate(unit) && index+1 < len(units) && isLowSurrogate(units[index+1]):
			builder.WriteRune(utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
		case isHighSurrogate(unit) || isLowSurrogate(unit):
			builder.WriteByte(0xE0 | byte(unit>>12))
			builder.WriteByte(0x80 | byte(unit>>6)&0x3F)
			builder.WriteByte(0x80 | byte(unit)&0x3F)
		default:
			builder.WriteRune(rune(unit))
		}
	}
	return builder.String()
}

// stringToUnits is the inverse of unitsToString. Bytes that are neither UTF-8 nor an encoded surrogate
// become U+FFFD, as Go's own conversion would make them.
func stringToUnits(text string) []uint16 {
	units := make([]uint16, 0, len(text))
	for index := 0; index < len(text); {
		if index+2 < len(text) &&
			text[index] == 0xED &&
			text[index+1] >= 0xA0 &&
			text[index+1] <= 0xBF &&
			text[index+2]&0xC0 == 0x80 {
			units = append(units, 0xD000|uint16(text[index+1]&0x3F)<<6|uint16(text[index+2]&0x3F))
			index += 3
			continue
		}
		character, size := utf8.DecodeRuneInString(text[index:])
		if character >= 0x10000 {
			high, low := utf16.EncodeRune(character)
			units = append(units, uint16(high), uint16(low))
		} else {
			units = append(units, uint16(character))
		}
		index += size
	}
	return units
}

// UnitLength is a Go string's length as JavaScript counts it, in UTF-16 code units.
func UnitLength(text string) int {
	return len(stringToUnits(text))
}

func isHighSurrogate(unit uint16) bool { return unit >= 0xD800 && unit <= 0xDBFF }
func isLowSurrogate(unit uint16) bool  { return unit >= 0xDC00 && unit <= 0xDFFF }

// characterAt is `text[index]`: the code unit, or -1 for undefined past either end.
func characterAt(text []uint16, index int) int {
	if index < 0 || index >= len(text) {
		return -1
	}
	return int(text[index])
}

// substring is `text.substring(start, end)`: both clamped to [0, length], swapped when start > end.
func substring(text []uint16, start int, end int) []uint16 {
	clampedStart := clamp(start, 0, len(text))
	clampedEnd := clamp(end, 0, len(text))
	if clampedStart > clampedEnd {
		clampedStart, clampedEnd = clampedEnd, clampedStart
	}
	return text[clampedStart:clampedEnd:clampedEnd]
}

// substr is `text.substr(start, length)`: a negative start counts from the end, the length is clamped.
func substr(text []uint16, start int, length int) []uint16 {
	clampedStart := start
	if start < 0 {
		clampedStart = max(len(text)+start, 0)
	}
	clampedStart = min(clampedStart, len(text))
	end := clamp(clampedStart+length, clampedStart, len(text))
	return text[clampedStart:end:end]
}

// slice is `text.slice(start, end)`: negatives count from the end, an end before the start is empty.
func slice(text []uint16, start int, end int) []uint16 {
	clampedStart := start
	if start < 0 {
		clampedStart = max(len(text)+start, 0)
	}
	clampedEnd := end
	if end < 0 {
		clampedEnd = max(len(text)+end, 0)
	}
	clampedStart = min(clampedStart, len(text))
	clampedEnd = min(clampedEnd, len(text))
	if clampedEnd < clampedStart {
		return text[clampedStart:clampedStart:clampedStart]
	}
	return text[clampedStart:clampedEnd:clampedEnd]
}

func clamp(value int, low int, high int) int {
	return min(max(value, low), high)
}

// includesUnit is `text.includes(character)` for one code unit.
func includesUnit(text []uint16, character uint16) bool {
	for _, unit := range text {
		if unit == character {
			return true
		}
	}
	return false
}

// equalsASCII is `text === literal` for an ASCII literal.
func equalsASCII(text []uint16, literal string) bool {
	if len(text) != len(literal) {
		return false
	}
	for index := range len(literal) {
		if text[index] != uint16(literal[index]) {
			return false
		}
	}
	return true
}

// concatUnits is `a + b + ...` over JavaScript strings.
func concatUnits(parts ...[]uint16) []uint16 {
	length := 0
	for _, part := range parts {
		length += len(part)
	}
	result := make([]uint16, 0, length)
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

// asciiUnits is an ASCII Go string literal as a JavaScript string.
func asciiUnits(text string) []uint16 {
	units := make([]uint16, len(text))
	for index := range len(text) {
		units[index] = uint16(text[index])
	}
	return units
}

// asciiView is the text for a regular expression whose every character class is ASCII: each ASCII unit
// as its byte, every other unit as 0xFF, a byte no ASCII class matches, so the view matches exactly
// where the JavaScript string does. Only a match's ASCII captures may be read back from it.
func asciiView(text []uint16) string {
	view := make([]byte, len(text))
	for index, unit := range text {
		if unit < utf8.RuneSelf {
			view[index] = byte(unit)
		} else {
			view[index] = 0xFF
		}
	}
	return string(view)
}

// trim is `text.trim()`.
func trim(text []uint16) []uint16 {
	start, end := 0, len(text)
	for start < end && ecmascripttext.IsWhitespace(rune(text[start])) {
		start++
	}
	for end > start && ecmascripttext.IsWhitespace(rune(text[end-1])) {
		end--
	}
	return text[start:end:end]
}

// jsonStringify is `JSON.stringify(text)` for a string: the short escapes, other controls as \u00xx,
// lone surrogates as \udxxx, everything else as itself.
func jsonStringify(units []uint16) string {
	var builder strings.Builder
	writeJSONString(&builder, units)
	return builder.String()
}

func writeJSONString(builder *strings.Builder, units []uint16) {
	builder.WriteByte('"')
	for index := 0; index < len(units); index++ {
		unit := units[index]
		switch {
		case unit == '"':
			builder.WriteString(`\"`)
		case unit == '\\':
			builder.WriteString(`\\`)
		case unit == '\b':
			builder.WriteString(`\b`)
		case unit == '\f':
			builder.WriteString(`\f`)
		case unit == '\n':
			builder.WriteString(`\n`)
		case unit == '\r':
			builder.WriteString(`\r`)
		case unit == '\t':
			builder.WriteString(`\t`)
		case unit < 0x20:
			fmt.Fprintf(builder, `\u%04x`, unit)
		case isHighSurrogate(unit) && index+1 < len(units) && isLowSurrogate(units[index+1]):
			builder.WriteRune(utf16.DecodeRune(rune(unit), rune(units[index+1])))
			index++
		case isHighSurrogate(unit) || isLowSurrogate(unit):
			fmt.Fprintf(builder, `\u%04x`, unit)
		default:
			builder.WriteRune(rune(unit))
		}
	}
	builder.WriteByte('"')
}
