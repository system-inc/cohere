// Package flagged holds comments the rule must report. Each case is its own comment group, a blank line apart, so
// no case lends another its context. Each shouted word is its own finding, at the word, and a want rides on the
// line the word is on: after a line comment, or inside a block comment on its own line, where analysistest reads it
// at the comment's opening line, so a +N names how many lines below that the word sits. Its pattern is in backticks,
// which the rule masks, so the want never adds a token of its own.
package flagged

// The TypeScript rule's firing cases, verbatim.

// NEVER cache this // want `^"NEVER" in a comment reads as shouting`

/* this is REALLY important */ // want `^"REALLY" in a comment`

// DO NOT EDIT // want `^"DO" in a comment` `^"NOT" in a comment` `^"EDIT" in a comment`

// this IS the one // want `^"IS" in a comment`

/** ALWAYS returns a copy */ // want `^"ALWAYS" in a comment`

// An unclosed backtick masks nothing after it, so its want is double-quoted: a backtick pattern would close it.

// `code and then NEVER do this // want "NEVER"

// The review's true positives, verbatim.

/*
 * wake and a delivery to a reader-less pipe already fails ENXIO and shrugs. Idempotent,
 * // want +1 `^"ENXIO" in a comment`
 */

/*
 * safe while watchers are armed BECAUSE the pipe is only the fast-path poke: the durable
 * // want +1 `^"BECAUSE" in a comment`
 */

/**
 * This YIELDS. It makes no claim about what finished, because it cannot know
 * // want +1 `^"YIELDS" in a comment`
 */

/*
 * OUTPUT MUST BE VALID INPUT. The name is printed in FULL, never fit() to the column width: a
 * // want +1 `^"OUTPUT" in` `^"MUST" in` `^"BE" in` `^"VALID" in` `^"INPUT" in` `^"FULL" in`
 */

/*
 * DO NOT rename this symbol to `positionsMissingPortrait` with the rest of the
 * // want +1 `^"DO" in a comment` `^"NOT" in a comment`
 */

// straight back in. A miss is a LOUD failure with near misses named by ID, never a silent // want `^"LOUD" in a`

// Apostrophes that are not quotes open nothing, verbatim.

// the users' data is NEVER cached by the dogs' walker // want `^"NEVER" in a comment`

// don't EVER cache what isn't yours // want `^"EVER" in a comment`

// the BOSS'S ORDERS' wording // want `^"BOSS" in a comment` `^"ORDERS" in a comment`

// a 'RENAME COLUMN'd table is NEVER safe // want `^"RENAME" in` `^"COLUMN" in` `^"NEVER" in`

// the '90s were LOUD and the users' data was quiet // want `^"LOUD" in a comment`

// the 'tis season, NEVER the 'quiet' one // want `^"NEVER" in a comment`

// 'NOT NULL' is ALWAYS enforced // want `^"ALWAYS" in a comment`

// Indentation that is not a code block, verbatim.

/*
This is prose
    and you must NEVER do it
// want +2 `^"NEVER" in a comment`
*/

/**
 * Run:
 *
 *     HOME=/x run
 *
 * Then ALWAYS check.
 * // want +5 `^"ALWAYS" in a comment`
 */

func run() int {
	/**
	 * You must NEVER do this.
	 * // want +1 `^"NEVER" in a comment`
	 */
	return 1
}

// Quotes that wrap but do not cover the shout, verbatim.

/*
 * the "quiet
 * phrase" is NEVER loud
 * // want +2 `^"NEVER" in a comment`
 */

/*
 * a 5" board
 * is NEVER warped
 * // want +2 `^"NEVER" in a comment`
 */

/*
 * the "first
 * line is NEVER
 * closed" here
 * // want +2 `^"NEVER" in a comment`
 */

// Each shouted word is a finding of its own, at the word.

// NEVER cache this, it is REALLY bad // want `^"NEVER" in a comment` `^"REALLY" in a comment`

// Go's notations are exempt only in their own shape.

// go:generate echo NEVER // want `^"NEVER" in a comment`

//lint:IGNORE this check // want `^"IGNORE" in a comment`

// the BUG(kirk) note is LOUD // want `^"BUG" in a comment` `^"LOUD" in a comment`

// BUG: the cache is stale after a fork // want `^"BUG" in a comment`

// A line comment group is one text to mask, and each of its lines reports its own tokens.

// the first line is LOUD // want `^"LOUD" in a comment`
// and the second is NOISY // want `^"NOISY" in a comment`

// Run:
//
//	HOME=/x run
//
// Then ALWAYS check. // want `^"ALWAYS" in a comment`

// The group is what tells this indented line from a code block: it follows prose, not a blank line.

// This is prose
//     and you must NEVER do it // want `^"NEVER" in a comment`

// A multi-byte rune inside masked code keeps every later line's place in the group, the shape of micromark's
// gfm_autolink_literal.go, so each later line reports its own tokens and reads no further than its own end.

// occur, which sounds like ASCII only, but they also support `www.點看.com`,
// and the line after it is NEVER quiet // want `^"NEVER" in a comment`

// ```
// 點看
// ```
// then ALWAYS check // want `^"ALWAYS" in a comment`

// An example's output is only output in a _test.go file.

func ExampleLoud() {
	// Output: LOUD // want `^"LOUD" in a comment`
}

// A block comment reports each word on the line it sits on, and multi-byte text before it, on an earlier line or on
// its own, in masked code or in prose, moves no finding off its word.

/*
 * the café's `naïve` cache
 * is NEVER warm, and the crème is ALWAYS cold
 * // want +2 `^"NEVER" in a comment` `^"ALWAYS" in a comment`
 */

// A word said twice is two findings, one at each.

// NEVER, and I mean NEVER // want `^"NEVER" in a comment` `^"NEVER" in a comment`
