package defer_in_infinite_loop

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5003, copied unchanged from staticcheck/sa5003/testdata/go1.0 into
// testdata/src/CheckDeferInInfiniteLoop, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which SA5003 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckDeferInInfiniteLoop")
}

// Over staticcheck's fixture and parity's, the second holding defers at every depth of a loop with no condition, in
// branches, switches, selects, nested loops and closures, the returns and breaks that spare a loop wherever they sit,
// and a generated file, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5003.Analyzer, Analyzer, analysistest.TestData(), "CheckDeferInInfiniteLoop",
		"parity")
}

// The differences fixture holds the loops where the two could most plausibly part, a labeled break or continue, a
// break that leaves only a nested loop, a panic in place of a return and a closure's own loop. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5003.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
