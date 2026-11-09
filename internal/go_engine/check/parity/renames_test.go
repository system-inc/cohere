package parity

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// renaming is a port that meets the wants fixture's want of a call to gained in its own words, which the want's
// pattern does not match, and every other want in the original's.
var renaming = calling(map[string]string{
	"shared": "call to shared",
	"gained": "gained called, in the port's words",
})

// gainedRenamed is the rename given beside the call that reads renaming's finding of the call to gained as the want's.
var gainedRenamed = Record{
	File:   "wants/wants.go",
	Line:   11,
	Text:   "renames: call to gained => gained called, in the port's words",
	Reason: "planted: the port names the call in its own words",
}

// A want the port meets in other words fails, and passes once those words are given beside the call as a rename of
// the want's.
func TestRequireUpstreamWantsMeetsAWantThroughARenameGivenBesideTheCall(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireUpstreamWants(recording, renaming, analysistest.TestData(), "wants")
	}), "wants.go:11:2: diagnostic \"gained called, in the port's words\" does not match pattern `call to gained`",
		"wants.go:11: no diagnostic was reported matching `call to gained`")
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Renaming(gainedRenamed).RequireUpstreamWants(recording, renaming, analysistest.TestData(), "wants")
	}))
}

// A rename given beside the call that no finding of the port's meets fails, named by its file and line, though every
// want is met.
func TestRequireUpstreamWantsFailsARenameThatDidNotHappen(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Renaming(gainedRenamed).RequireUpstreamWants(recording, port, analysistest.TestData(), "wants")
	}), "wants/wants.go:11 is given beside the call the rename \"call to gained\" => \"gained called, in the port's "+
		"words\", and the port finds nothing there in those words")
}

// A rename given beside the call with no reason fails, named by its file and line, though it is met.
func TestRequireUpstreamWantsFailsARenameWithNoReason(test *testing.T) {
	test.Parallel()
	unreasoned := gainedRenamed
	unreasoned.Reason = " "
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Renaming(unreasoned).RequireUpstreamWants(recording, renaming, analysistest.TestData(), "wants")
	}), "wants/wants.go:11 is given beside the call a record with no reason: write why in its Reason")
}

// A record of any other kind given beside the call fails, named, since only a rename reads a finding as a want.
func TestRequireUpstreamWantsFailsARecordThatRenamesNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Renaming(Record{File: "wants/wants.go", Line: 11, Text: "finds: call to gained", Reason: "planted"}).
			RequireUpstreamWants(recording, port, analysistest.TestData(), "wants")
	}), "wants/wants.go:11 is given beside the call a record that renames nothing, where only `renames:` reads a "+
		"finding of the port's as a want: \"finds: call to gained\"")
}

// RequireUpstreamWantsWithFixes reads its renames as RequireUpstreamWants does, the fixes held as before.
func TestRequireUpstreamWantsWithFixesMeetsAWantThroughARename(test *testing.T) {
	test.Parallel()
	inOtherWords := calling(map[string]string{"shared": "shared called, in other words"},
		replacing("replace with kept", "kept()"))
	renamed := []Record{}
	for _, line := range []int{10, 11} {
		renamed = append(renamed, Record{File: "fixes/fixes.go", Line: line,
			Text: "renames: call to shared => shared called, in other words", Reason: "planted: other words"})
	}
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Renaming(renamed...).RequireUpstreamWantsWithFixes(recording, inOtherWords, analysistest.TestData(),
			"fixes")
	}))
}
