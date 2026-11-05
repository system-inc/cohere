// Package flagged holds a dash in every place the rule reads one. A want rides on the line the dash sits on, and a
// dash on a later line of a block comment or a raw string is wanted from the line above it with +1 or +2, so each
// case also proves the report lands on the dash's own line rather than the comment's or the literal's first.
package flagged

// Measure is the one function here — and its doc comment carries the dash. // want `^This is an em-dash\.`
func Measure() int { return 1 }

// a line comment with a page range, 3–7, inside it // want `^This is an en-dash\.`

// two dashes — one comment — two findings // want `^This is an em-dash\.` `^This is an em-dash\.`

/* a block comment — on one line */ // want `^This is an em-dash\.`

// want +2 `^This is an en-dash\.`
/*
 * a block comment whose second line holds the dash, pages 3–7
 */

var interpreted = "an interpreted string — with a dash" // want `^This is an em-dash\.`

var interpretedEnDash = "pages 3–7" // want `^This is an en-dash\.`

var raw = `a raw string — with a dash` // want `^This is an em-dash\.`

// want +2 `^This is an en-dash\.`
var rawAcrossLines = `a raw string
whose second line holds the dash, pages 3–7`

var escaped = "an escape \u2014 spells it" // want `^This string spells an em-dash with an escape`

var escapedLong = "a long escape \U00002013 spells it" // want `^This string spells an en-dash with an escape`

var escapedHex = "three hex escapes \xe2\x80\x94 spell it" // want `^This string spells an em-dash with an escape`

var escapedOctal = "three octal escapes \342\200\223 spell it" // want `^This string spells an en-dash with an escape`

// want +1 `^This is an em-dash\.` `^This string spells an em-dash with an escape`
var writtenAndEscaped = "one written — and one escaped \u2014"

func use() []any {
	return []any{interpreted, interpretedEnDash, raw, rawAcrossLines, escaped, escapedLong, escapedHex, escapedOctal,
		writtenAndEscaped}
}
