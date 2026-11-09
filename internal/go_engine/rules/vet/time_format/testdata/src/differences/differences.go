// Package differences holds the two places the port parts from the original, where the original is wrong. A literal
// whose escapes come before the mistake or inside it: the original takes the mistake's index in the literal's
// decoded value for an offset into its source, so its finding covers the wrong bytes and its fix rewrites them, while
// the port's covers the bytes that spell the mistake and its fix replaces those alone. And a method expression, whose
// call takes the receiver first: the original reads the receiver as the layout and judges nothing, while the port
// reads the layout after it. differences.go.fixed is this file as every fix the port offers in it leaves it, byte for
// byte, so each fix's bytes are held, and each record places its finding and names its fix's message.
package differences

import "time"

// layout is a named constant, reported at its name with no fix wherever it is the layout.
const layout = "2006-02-01"

// goodLayout is a named constant with no mistake in it.
const goodLayout = "2006-01-02"

// holder embeds time.Time, so its Format is time.Time's, promoted.
type holder struct {
	time.Time
}

// aliased is an alias for time.Time, whose methods are time.Time's.
type aliased = time.Time

// An escape before the mistake is more bytes of source than of value, so the original lands early.
func before(moment time.Time) {
	// A tab's escape is two bytes for one, and the original's fix leaves `"\2006-01-021"`, an octal escape.
	_ = moment.Format("\t2006-02-01") // port moves at column 23 to 33 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// A hexadecimal escape is four bytes for one.
	_ = moment.Format("\x41 2006-02-01") // port moves at column 26 to 36 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// An octal escape is four bytes for one.
	_ = moment.Format("\101 2006-02-01") // port moves at column 26 to 36 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// A short Unicode escape is six bytes for two.
	_ = moment.Format("\u00e9 2006-02-01") // port moves at column 28 to 38 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// A long Unicode escape is ten bytes for four.
	_ = moment.Format("\U0001F600 2006-02-01") // port moves at column 32 to 42 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// An escaped quote is two bytes for one.
	_ = moment.Format("\"2006-02-01\"") // port moves at column 23 to 33 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// An escaped backslash is two bytes for one, in Parse's layout.
	_, _ = time.Parse("\\ 2006-02-01", "2021-01-01") // port moves at column 24 to 34 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// Two escapes, so the original lands two bytes early.
	_ = moment.Format("\t\t2006-02-01") // port moves at column 25 to 35 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
}

// An escape inside the mistake spells it in more bytes than its ten, so the original covers too few.
func inside(moment time.Time) {
	// An escaped zero in the middle, and the original's fix leaves `"2006-01-02-01"`.
	_ = moment.Format("2006-\x302-01") // port moves at column 21 to 34 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// The mistake's first byte escaped: the two start together, and the original ends early.
	_ = moment.Format("\x32006-02-01") // port moves at column 21 to 34 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// Its last byte escaped, and the original's fix leaves `"2006-01-02x31"`.
	_ = moment.Format("2006-02-0\x31") // port moves at column 21 to 34 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
	// Both dashes escaped, six bytes each for one.
	_ = moment.Format("2006\u002d02\u002d01") // port moves at column 21 to 41 with "Replace 2006-02-01 with 2006-01-02": 2006-02-01 should be 2006-01-02
}

// A method expression takes the receiver first and the layout after it, where the original never looks.
func methodExpressions(moment time.Time, pointer *time.Time, embedded holder, variable string) {
	// time.Time's own method expression, with the fix.
	_ = time.Time.Format(moment, "2006-02-01") // port finds at column 32 to 42: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02
	// The method expression of a pointer's method set.
	_ = (*time.Time).Format(pointer, "2006-02-01") // port finds at column 36 to 46: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02
	// A parenthesized method expression.
	_ = (time.Time.Format)(moment, "2006-02-01") // port finds at column 34 to 44: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02
	// A method expression through an alias for time.Time.
	_ = aliased.Format(moment, "2006-02-01") // port finds at column 30 to 40: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02
	// A method expression of the Format holder's embedding promotes.
	_ = holder.Format(embedded, "Mon 2006-02-01") // port finds at column 35 to 45: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02
	// A named constant, at its name, with no fix, as every layout that is not one literal.
	_ = time.Time.Format(moment, layout) // port finds at column 31, no end: 2006-02-01 should be 2006-01-02
	// An escape before the mistake, in a method expression, where the original judges nothing.
	_ = time.Time.Format(moment, "\t2006-02-01") // port finds at column 34 to 44: 2006-02-01 should be 2006-01-02 // port fixes with "Replace 2006-02-01 with 2006-01-02": 2006-01-02

	_ = time.Time.Format(moment, "2006-01-02")
	_ = time.Time.Format(moment, variable)
	_ = (*time.Time).Format(pointer, goodLayout)
}
