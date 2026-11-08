package oversized_shift

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9006"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackage is staticcheck's own fixture for SA9006, copied unchanged from its testdata/go1.0 into
// testdata/src. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstreamPackage = "CheckStaticBitShift"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackage)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// the check depends on and a generated file, which neither filters.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9006.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree: shifts nested one in another,
// assigned through several targets, and shifted by the largest amounts a constant can name. It records no difference,
// since the two read the same form and differ nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9006.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
