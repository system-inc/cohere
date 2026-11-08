package deferred_lock_after_lock

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa2003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA2003, copied unchanged from its testdata/go1.0 into testdata/src. The
// fixture uses nothing newer than Go 1.0, so it needs no version of its own.
const upstream = "CheckDeferLock"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2003.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree, a deferred method other than the
// first call's or reached through another form, and a lock or a defer at a block's edge. It records no difference,
// since the two read the same form and differ nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2003.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
