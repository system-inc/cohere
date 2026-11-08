package time_format

import (
	"slices"
	"testing"
)

// unclaimedFindings is the findings one side alone makes that no record of a differences fixture has claimed yet.
type unclaimedFindings []finding

// claim removes the one finding on pinned's line with pinned's message and returns it, and fails the test, returning
// nil, unless there is exactly one.
func (unclaimed *unclaimedFindings) claim(test *testing.T, side string, pinned finding) *finding {
	test.Helper()
	matching := []int{}
	for index, found := range *unclaimed {
		if found.file == pinned.file && found.line == pinned.line && found.message == pinned.message {
			matching = append(matching, index)
		}
	}
	if len(matching) != 1 {
		test.Errorf("%s:%d records that %s alone finds %q there once, and it does %d times", pinned.file, pinned.line,
			side, pinned.message, len(matching))
		return nil
	}
	claimed := (*unclaimed)[matching[0]]
	*unclaimed = slices.Delete(*unclaimed, matching[0], matching[0]+1)
	return &claimed
}
