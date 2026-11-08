package redundant_slice_length

import (
	"testing"

	"honnef.co/go/tools/simple/s1010"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1010, copied unchanged from simple/s1010/testdata/go1.0/CheckSlicing
// into testdata/src/CheckSlicing with its golden file. The go1.0 folder sets the language version of staticcheck's
// run, which S1010 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSlicing"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the query branches on and the near miss of each, and a generated file, and their findings must be the same
// multiset, each the same to its end and its fixes.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1010.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, a type parameter's slice, a
// string and a pointer to an array, a variable shadowed in an inner scope, a len declared by a package or a
// parameter, conversions and a function literal, and records none, since the port parts from the original nowhere.
// The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree
// there finding for finding.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1010.Analyzer, Analyzer, analysistest.TestData(), "differences",
		"differences/length")
}
