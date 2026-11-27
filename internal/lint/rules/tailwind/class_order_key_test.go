package tailwind

import (
	"strings"
	"testing"
)

// TestCompareTailwindCandidatesIsUpstreamsCompare holds compareTailwindCandidates to `utils/compare.ts` at
// Tailwind 4.3.3 on the cases its comment names. Each wanted sign is upstream's own function run under Node.
func TestCompareTailwindCandidatesIsUpstreamsCompare(test *testing.T) {
	test.Parallel()
	nines, eights := strings.Repeat("9", 400), strings.Repeat("9", 399)+"8"

	for _, testCase := range []struct {
		name, left, right string
		want              int
	}{
		{"digit runs compare as numbers", "swell-5", "swell-15", -1},
		{"a run against a longer run", "a-2x", "a-10", -1},
		{"a decimal is two runs", "p-1.5", "p-1.25", -1},
		{"after an equal run the rest decides", "a-10b", "a-10a", 1},
		{"a prefix sorts first", "bg-red-500/70", "bg-red-500", 1},
		{"one number spelled two ways falls back to the spelling", "w-0123", "w-123", -1},
		{"numbers a double cannot tell apart fall back to the spelling", "x-9007199254740993", "x-9007199254740992", 1},
		{"two infinities subtract to NaN and fall back to the spelling", "x-" + eights, "x-" + nines, -1},
		{"a non-ASCII difference", "a-é", "a-e", 1},
		{"UTF-16 order where UTF-8 order disagrees", "a-～", "a-\U0001f600", 1},
	} {
		got := compareTailwindCandidates(testCase.left, testCase.right)
		if (got < 0) != (testCase.want < 0) || (got > 0) != (testCase.want > 0) {
			test.Errorf("%s: compare(%.20q, %.20q) has sign of %d, upstream's has %d", testCase.name, testCase.left,
				testCase.right, got, testCase.want)
		}
		if reversed := compareTailwindCandidates(testCase.right, testCase.left); (reversed < 0) != (testCase.want > 0) {
			test.Errorf("%s: reversed, compare has sign of %d, want the opposite of %d", testCase.name, reversed,
				testCase.want)
		}
	}
}
