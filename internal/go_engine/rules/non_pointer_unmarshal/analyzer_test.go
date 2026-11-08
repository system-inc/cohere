package non_pointer_unmarshal

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1014"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1014, copied unchanged from staticcheck/sa1014/testdata/go1.0/CheckUnmarshalPointer
// into testdata/src/CheckUnmarshalPointer, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which no part of SA1014 reads, so
// the fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckUnmarshalPointer")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the shapes
// staticcheck's representation builds an argument or a call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1014.Analyzer, Analyzer, analysistest.TestData(), "CheckUnmarshalPointer", "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1014.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
