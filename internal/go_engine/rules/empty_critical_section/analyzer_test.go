package empty_critical_section

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa2001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA2001, copied unchanged from
// staticcheck/sa2001/testdata/go1.0/CheckEmptyCriticalSection into testdata/src/CheckEmptyCriticalSection. The go1.0
// folder sets the language version of staticcheck's run, which SA2001 never reads, so the fixture loads from a tree
// laid out by import path as the house's fixtures do.
const upstream = "CheckEmptyCriticalSection"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity's and one named as the sync package's external tests, which the original
// exempts, the original and the port find the same findings. parity holds every shape of call and receiver the check
// reads.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2001.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity", "sync_test")
}

// The differences fixture holds the receivers where the two could most plausibly part, two values that print alike, a
// generic instantiation, a comment inside a receiver and a promoted method. It records no difference, since the port
// parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
