package discarded_pure_call

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4017, copied unchanged from
// staticcheck/sa4017/testdata/go1.0/CheckSideEffectFreeCalls into testdata/src/CheckSideEffectFreeCalls, its test file
// included. The go1.0 folder sets the language version of staticcheck's run, which SA4017 never reads, so the fixture
// loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSideEffectFreeCalls"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity's and one at the import path fmt_test, which the original spares fmt.Sprintf in,
// the original and the port find the same findings. parity holds pure and impure calls, calls with no static callee,
// results read and discarded, and the benchmark exception in a test file, in an external test file, outside one and
// under a line directive naming one.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4017.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity", "fmt_test")
}

// The differences fixture holds the calls where the purity judgment could most plausibly part. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4017.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
