// Package flagged holds literals the rule must report: each names a home directory in a _test.go file, and nothing
// excuses it. Each want comment is itself a comment group on the literal's line, so no want here spells the marker,
// which would excuse the very literal it expects.
package flagged

import "testing"

// A macOS home path.
const macRoot = "/Users/someone/Projects/ahra" // want `^This test names a home directory, so it reads a path`

// A Linux home path, in a raw string.
const linuxRoot = `/home/someone/checkouts/ahra` // want `This test names a home directory`

// A home path inside a longer string.
var line = "error at /Users/someone/a.ts:1:1" // want `This test names a home directory`

// Not a machine path: planted cases.
var roots = []string{"/Users/a/x"}
var other = "/Users/c/z" // want `This test names a home directory`

// Not a machine path:
const bare = "/Users/someone/x" // want `^This home directory is marked as not a machine path and gives no reason\.`

// Not a machine path:
// a reason on the group's next line is no reason, since the reason is what follows the marker on its own line.
const reasonOnTheNextLine = "/Users/someone/x" // want `^This home directory is marked`

// not a machine path: the marker is spelled as written, so a lowercase one excuses nothing.
const lowerCaseMarker = "/Users/someone/x" // want `This test names a home directory`

// A sentence that says Not a machine path: mid-line only mentions the marker, which excuses nothing.
const markerInASentence = "/Users/someone/x" // want `This test names a home directory`

// A marker trailing the code on the line above speaks for that line, not for the declaration below it.
var aboveTheTrailed = "/repo"         // Not a machine path: this speaks for its own line.
var trailedAbove = "/Users/someone/x" // want `This test names a home directory`

// A blank line between the marker and the declaration leaves the marker above nothing.
// Not a machine path: planted with a gap.

var gapped = "/Users/someone/x" // want `This test names a home directory`

// A marker on the line below a literal is a group of its own, starting below it, so it speaks for the line under it.
var below = "/Users/someone/x" // want `This test names a home directory`
// Not a machine path: this speaks for the declaration under it.
var underTheMarker = "/repo"

func TestEachStatementIsItsOwn(test *testing.T) {
	test.Parallel()
	// Not a machine path: the first statement only.
	first := "/Users/someone/first"
	second := "/home/someone/second" // want `This test names a home directory`
	test.Log(first, second)
}

func TestMarkerWithoutReasonAboveAStatement(test *testing.T) {
	test.Parallel()
	/* Not a machine path: */
	test.Log("/Users/someone/x") // want `^This home directory is marked`
}
