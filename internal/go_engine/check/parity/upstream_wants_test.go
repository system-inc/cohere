package parity

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// A port whose fix makes the golden file passes, its wants met as well.
func TestRequireUpstreamWantsWithFixesPassesFixesThatMakeTheGoldenFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWantsWithFixes(recording, keeping, analysistest.TestData(), "fixes")
	}))
}

// A golden file the fixes do not make fails, naming the file.
func TestRequireUpstreamWantsWithFixesFailsAWrongGoldenFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWantsWithFixes(recording, keeping, analysistest.TestData(), "fixes_wrong")
	}), "all fixes: unexpected testdata/src/fixes_wrong/fixes_wrong.go content:")
}

// As RunWithSuggestedFixes holds them, a fix in a file with no golden file fails on reading it, and a port that
// offers no fix passes, with a golden file or without, since only a file some fix edits is held.
func TestRequireUpstreamWantsWithFixesHoldsOnlyAFileAFixEdits(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWantsWithFixes(recording, keeping, analysistest.TestData(), "fixes_unwritten")
	}), "fixes_unwritten.go.golden: ")
	for _, fixture := range []string{"fixes_unwritten", "fixes"} {
		requireFailures(test, failuresOf(test, func(recording testing.TB) {
			RequireUpstreamWantsWithFixes(recording, calling(sharedMessages), analysistest.TestData(), fixture)
		}))
	}
}
