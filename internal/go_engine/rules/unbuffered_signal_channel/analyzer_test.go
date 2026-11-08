package unbuffered_signal_channel

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1017, copied unchanged from
// staticcheck/sa1017/testdata/go1.0/CheckUnbufferedSignalChan into testdata/src/CheckUnbufferedSignalChan. The go1.0
// folder sets the language version of staticcheck's run, which no part of SA1017 reads, so the fixture loads from a
// tree laid out by import path as the house's fixtures do.
const upstream = "CheckUnbufferedSignalChan"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// staticcheck's representation builds an argument or a call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1017.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1017.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
