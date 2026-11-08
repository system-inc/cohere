package nan_comparison

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixture for SA4012, copied unchanged from its testdata/go1.0 into testdata/src/CheckNaNComparison,
// holds the port to every `//@ diag(...)` note the original expects, and to nothing more. The fixture uses nothing
// newer than Go 1.0, so it loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckNaNComparison")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// the check depends on: which operations it judges, and how far it follows an operand back to a call of math.NaN.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4012.Analyzer, Analyzer, analysistest.TestData(), "CheckNaNComparison", "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree, operands that reach the call across
// loops and branches, through a phi or none, where a flattening that stopped short or went too far would show. It
// records no difference, since the two read the same form and differ nowhere, so they must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4012.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
