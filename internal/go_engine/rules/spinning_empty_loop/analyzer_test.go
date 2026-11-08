package spinning_empty_loop

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5002, copied unchanged from staticcheck/sa5002/testdata/go1.0/CheckInfiniteEmptyLoop
// into testdata/src/CheckInfiniteEmptyLoop, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which SA5002 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckInfiniteEmptyLoop")
}

// Over staticcheck's fixture and parity's, the second holding every kind of condition the side effects judgment
// answers, the constants the original spares, the loops it leaves alone and a generated file, the original and the
// port find the same findings, so the spin's short range is held as well as its start.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5002.Analyzer, Analyzer, analysistest.TestData(), "CheckInfiniteEmptyLoop",
		"parity")
}

// The differences fixture holds the loops where the two could most plausibly part, a `false` the code declares, a
// spared constant of a type of its own, a constant compared, a builtin or a conversion in the condition. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5002.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
