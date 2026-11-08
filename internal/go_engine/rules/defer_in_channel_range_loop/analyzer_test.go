package defer_in_channel_range_loop

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa9001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamFixtures are staticcheck's own fixtures for SA9001, copied unchanged from staticcheck/sa9001/testdata into
// testdata/src with their version folders kept, since both versions hold a package of the same name: go1.0's, and
// go1.18's, whose loop ranges over a type parameter. A version folder sets the language version of staticcheck's run,
// which SA9001 never reads, and a fixture loaded outside a module may use generics, so each loads from a tree laid out
// by import path as the house's fixtures do, the folder part of its path.
var upstreamFixtures = []string{
	"go1.0/CheckDubiousDeferInChannelRangeLoop",
	"go1.18/CheckDubiousDeferInChannelRangeLoop",
}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamFixtures...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds loops over
// channels of every shape the core type reads, named, receive only, converted and generic, loops over what is not a
// channel, defers at every depth and in closures, the returns and breaks that spare a loop, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9001.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamFixtures), "parity")...)
}

// The differences fixture holds the loops where the two could most plausibly part, a continue after the loop's return
// or break, which both judge as never ending, a labeled break, a goto and an approximate channel constraint. It records
// no difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
