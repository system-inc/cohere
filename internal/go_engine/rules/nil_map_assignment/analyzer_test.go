package nil_map_assignment

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5000, copied unchanged from its testdata/go1.0 into testdata/src/CheckNilMaps, holds
// the port to every `//@ diag(...)` note the original expects, and to nothing more. The fixture uses nothing newer
// than Go 1.0, so it needs no version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckNilMaps")
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, how a map an update
// writes into was made and the phis it passed through on the way, the original and the port find the same findings,
// the end of the update's syntax held as well as its start.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5000.Analyzer, Analyzer, analysistest.TestData(), "CheckNilMaps", "parity")
}

// The differences fixture holds the cases where the two could plausibly part, maps that pass through many phis, in
// loops and in branches, where a port that flattened them any other way would show a finding one side makes and the
// other does not. It records no difference, since the two read the same form and differ nowhere, so the two must agree
// there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
