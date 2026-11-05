// Package allowed holds literals the rule must leave alone: each names a home directory and a reasoned marker
// excuses it, or it names none.
package allowed

import "testing"

// A repository path is no home directory.
const repositoryRoot = "/repo/app/layout.tsx"

// Not a machine path: the input whose home prefix is trimmed.
const excusedAbove = "/Users/someone/x"

// Not a machine path: planted cases.
var excusedBlock = []string{
	"/Users/a/x",
	"/home/b/y",
}

const excusedAtTheEnd = "/Users/someone/x" // Not a machine path: trimmed by the transform.

const excusedByABlockComment = "/Users/someone/x" /* Not a machine path: trimmed by the transform. */

// Not a machine path:
// the reason may open the group's next line, as testpolicy reads the group's whole text.
const reasonOnTheNextLine = "/Users/someone/x"

// not a machine path: testpolicy reads the marker in any case.
const lowerCaseMarker = "/Users/someone/x"

// Testpolicy finds the marker anywhere in the group, so this sentence saying Not a machine path: a planted case,
// excuses the declaration below it as testpolicy's does.
const markerInASentence = "/Users/someone/x"

// Not a machine path: every case below is a planted source, never read from the filesystem.
func TestExcusedAboveTheFunction(test *testing.T) {
	test.Parallel()
	cases := []string{
		"/Users/someone/Projects/ahra",
		"/home/someone/checkouts/ahra",
	}
	test.Log(cases)
}

func TestExcusedAboveAStatement(test *testing.T) {
	test.Parallel()
	// Not a machine path: planted inside the test, never read.
	cases := []string{
		"/Users/someone/Projects/ahra",
		"/home/someone/checkouts/ahra",
	}
	test.Log(cases, excusedAbove, excusedBlock, excusedAtTheEnd, excusedByABlockComment)
}
