// Package allowed holds comments the rule must leave alone. Each case is its own comment group, a blank line apart,
// so no case lends another its context.
package allowed

// The TypeScript rule's silent cases, verbatim.

// parse the body before validating it

// the JSON body arrives over HTTP from the API

// send it over RPC to the DB

// the `NOT_FOUND` branch returns early

// the `NEVER` constant is a sentinel

// read DATABASE_URL from the environment

// the H3 heading and the C1 column

// amounts in KRW have no minor unit

// "ORDER STATUS" is the column label

// pass 'US' as the region

/*
 * ```
 * NEVER DO THIS
 * ```
 */

/*
 * git commit --amend NEVER
 */

// the IP address and the OS version

// TODO: handle the empty case

// The acronyms the review found, verbatim.

/**
 * A freshly started Codex TUI writes session metadata before the human has typed
 */

// adapter projects its native TUI into the same two columns.

/*
 * kept dialing .176 on port 7442 for six months (seen on the wire 2026-10-01 as ARP who-has every
 */

// XML character references are literal inside CDATA. Unwrap the one admitted

// Writes `text` spoken by `voice` (an identifier or a voice name) to a CAF file at `outputPath`

// FHIR interpretation code: "N" = Normal, "A"/"AA"/"H"/"HH"/"L"/"LL" = abnormal.

// Quoted capital phrases, verbatim.

// a 'RENAME COLUMN' statement is not idempotent

// 'ON CONFLICT DO NOTHING' keeps the first row

// the pragma reads 'SYNCHRONOUS FULL', then commits

// Indented code blocks, verbatim.

/**
 * Run it sandboxed:
 *
 *     TMPDIR=$HOME/tmp sandbox-exec -f profile.sb node run.mjs
 */

/*
Run it:

	HOME=/x TMPDIR=/y node run.mjs
*/

/**
 * Run it:
 * ```
 * TMPDIR=$HOME/tmp node run.mjs
 * ```
 */

// Quotes that wrap behind a gutter, verbatim.

/*
 * Kept apart from the token pattern above so prose stays safe: the run must start and end on a
 * capital, digit or underscore and hold only capitals and token punctuation, so "the caller's job
 * and you must NOT" still cannot pair its apostrophe with a later one. Mirrors cohere's shouting.go.
 */

/*
 * "ok" then "a phrase that
 * says NEVER" here
 */

// A line comment group is one text to mask, as a block comment is: the same shapes written with slashes.

// Run it sandboxed:
//
//	TMPDIR=$HOME/tmp sandbox-exec -f profile.sb node run.mjs

// Example:
//
//	FOO := LOUD

// ```
// NEVER DO THIS
// ```

// the caller's job is "quiet
// and you must NOT" still

// the `NOT
// FOUND` branch returns early

// git commit --amend NEVER

// Directives are a tool's words.

//go:generate echo NEVER EVER

//cohere:ignore BECAUSE REASONS

//lint:ignore SA1019 BECAUSE THE OLD API STAYS

//export LOUDLY

// Thing is read in prose, and the directive gofmt moves to the end of its doc comment is not.
//
//go:generate echo LOUD NOISE
var Thing = 1

// go/doc notes open with their marker, which is notation.

// BUG(kirk): the cache is stale after a fork.

// HACK(kirk): this waits on the old index.

/* BUG(kirk): the block form of a note. */

// Deprecated: use Thing instead.
var Older = 1
