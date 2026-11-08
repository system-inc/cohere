package overlapping_encode_buffers

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa1031"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are staticcheck's own fixtures for SA1031, one for each encoder the original judges, copied
// unchanged from staticcheck/sa1031/testdata/go1.0 into testdata/src.
var upstreamPackages = []string{
	"CheckEncodingAscii85", "CheckEncodingBase32", "CheckEncodingBase64", "CheckEncodingHex",
}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
// The go1.0 folder sets the language version of staticcheck's run, which no part of SA1031 reads, so the fixtures load
// from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's own fixtures and parity's, the original and the port find the same findings. parity holds
// buffers that are one value, one value reached through a phi, slices of one array from one bound, from bounds that
// differ and from bounds a phi or a loop gives, and nil, for each encoder, and the shapes staticcheck's
// representation builds the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1031.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, []string{"parity"})...)
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck and records no
// difference, since there is none, so the two must find the same findings there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1031.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
