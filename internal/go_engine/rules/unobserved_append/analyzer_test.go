package unobserved_append

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4010"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixture for SA4010, copied unchanged from its testdata/go1.0 into
// testdata/src/CheckIneffectiveAppend, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The fixture uses nothing newer than Go 1.0, so it loads from a tree laid out by import path as the
// house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckIneffectiveAppend")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// the check depends on: where an append's result goes, and where the slice it extends came from.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, sa4010.Analyzer, Analyzer, analysistest.TestData(), "CheckIneffectiveAppend", "parity",
	)
}

// The differences fixture holds the cases where the two could plausibly disagree, appends built from many values
// through loops and phis, where an outcome that hung on the order the original ranges over a map would show. It
// records no difference, since the two read the same form and differ nowhere, so they must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4010.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
