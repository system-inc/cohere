package ineffective_copy

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4001, copied unchanged from staticcheck/sa4001/testdata/go1.0/CheckIneffectiveCopy
// into testdata/src/CheckIneffectiveCopy, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which SA4001 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckIneffectiveCopy")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds every shape
// the two patterns accept or refuse, parentheses at each level, the names cgo generates beside names that only
// resemble them, and the star of a type.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4001.Analyzer, Analyzer, analysistest.TestData(), "CheckIneffectiveCopy", "parity")
}

// The differences fixture holds the expressions where the port's test of syntax could most plausibly part from the
// patterns, parentheses at every level, a cgo name behind parentheses, a dereference of a call or a conversion and a
// chain of the two operators. It records no difference, since the port parts from the original nowhere, so the two
// must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
