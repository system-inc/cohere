package ip_compared_as_bytes

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1021"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1021, copied unchanged from staticcheck/sa1021/testdata/go1.0/CheckBytesEqualIP
// into testdata/src/CheckBytesEqualIP, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which no part of SA1021 reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckBytesEqualIP")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// staticcheck's representation builds a net.IP, its conversion to []byte or the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1021.Analyzer, Analyzer, analysistest.TestData(), "CheckBytesEqualIP", "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1021.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
