package ineffective_break

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4011"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixture for SA4011, copied unchanged from staticcheck/sa4011/testdata/go1.0/CheckScopedBreak into
// testdata/src/CheckScopedBreak, holds the port to every `//@ diag(...)` note the original expects, and to nothing
// more. The go1.0 folder sets the language version of staticcheck's run, which SA4011 never reads, so the fixture
// loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckScopedBreak")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds breaks that
// end switch and select cases in for and range loops, in an if and its else, in a loop inside a closure, the breaks
// the original leaves alone, labeled, not last, nested deeper, in a type switch or outside any loop, and a generated
// file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4011.Analyzer, Analyzer, analysistest.TestData(), "CheckScopedBreak", "parity")
}

// The differences fixture holds the breaks where the two could most plausibly part, a switch with an initializer, a
// select whose break ends its second case, a labeled switch and an else if. It records no difference, since the port
// parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4011.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
