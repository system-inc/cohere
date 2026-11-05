// Package lookalike holds characters that look like a dash and are not one, and a dash the rule does not read.
package lookalike

// The minus sign, U+2212, is arithmetic: 7 − 3 is 4.

var minus = "7 − 3"

var escapedMinus = "7 \u2212 3"

// The Unicode hyphen, U+2010, joins a well‐formed compound as the hyphen-minus does.

var hyphen = "well‐formed"

// A rune literal names a character, as a rule that looks for dashes must, so it is code, not prose.

var emDash = '—'

var escapedEnDash = '\u2013'

func use() []any { return []any{minus, escapedMinus, hyphen, emDash, escapedEnDash} }
