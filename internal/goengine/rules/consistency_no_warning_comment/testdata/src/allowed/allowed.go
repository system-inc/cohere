// Package allowed holds comments the rule must leave alone: directives, and the words where a comment only
// mentions them, in backticks.
package allowed

//go:generate go run ./tools/TODO

//lint:ignore U1000 kept until the FIXME upstream lands
func kept() {}

// A directive's arguments are its tool's, a reason after them included.
//
//nolint:gocritic // the XXX in this reason rides on the directive
var quiet = 1

//cohere:ignore cohere-go/consistency-no-warning-comment TODO is the subject of this line
var subject = 2

// The rule reads `TODO`, `FIXME` and `XXX` as tasks, and these are mentions of them.

/* A block comment mentions `FIXME` the same way. */

// A doc comment may wrap a backtick pair across its lines, `as in this
// TODO` here, and the pair still holds the word.

/*
 * A backtick span in a block comment may wrap too: `the
 * FIXME` is still inside it.
 */

// Code that writes the word is not a comment: the rule reads comments only.
var marker = "TODO"
