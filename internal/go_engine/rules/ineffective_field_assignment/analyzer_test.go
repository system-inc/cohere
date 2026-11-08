package ineffective_field_assignment

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4005, copied unchanged from its testdata/go1.0 into
// testdata/src/CheckIneffectiveFieldAssignments, holds the port to every `//@ diag` note the original expects, and to
// nothing more. The fixture uses nothing newer than Go 1.0, so it needs no version of its own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckIneffectiveFieldAssignments")
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings. Staticcheck's report gives a finding the end of its store's syntax as well as its
// start, so the end is held too.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4005.Analyzer, Analyzer, analysistest.TestData(),
		"CheckIneffectiveFieldAssignments", "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree. It records no difference, since the
// two read the same form and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4005.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
