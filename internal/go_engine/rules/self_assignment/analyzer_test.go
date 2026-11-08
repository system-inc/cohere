package self_assignment

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4018, copied unchanged from
// staticcheck/sa4018/testdata/go1.0/CheckSelfAssignment into testdata/src/CheckSelfAssignment. The go1.0 folder sets
// the language version of staticcheck's run, which SA4018 never reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
const upstream = "CheckSelfAssignment"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds every kind
// of side the side effects judgment answers, pure and impure calls among them, the assignments the original leaves
// alone, sides that print alike or apart, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4018.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the sides where the purity judgment and the rendering could most plausibly part. It
// records no difference, since the port parts from the original nowhere, so the two must agree there finding for
// finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4018.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
