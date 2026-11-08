package hoisted_map_key_conversion

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa6001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA6001, its files copied unchanged from testdata/go1.0 and testdata/go1.18
// into one package under testdata/src. Staticcheck's runner loads each version directory as a module of its own at
// that Go version; the two files share no name and need nothing of a version but the type parameters the second
// declares, which the toolchain's own version, newer than both, allows, so one package holds them.
const upstream = "CheckMapBytesKey"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6001.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the cases where the two could plausibly disagree, and records no difference, since
// the two read the same form and differ nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
