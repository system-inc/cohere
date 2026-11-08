package unsupported_marshal_type

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa1026"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are staticcheck's own fixture for SA1026, copied unchanged from staticcheck/sa1026/testdata into
// testdata/src, its two folders one package of the same name each, so each keeps its version folder as the first
// element of its import path. The version folder sets the language version of staticcheck's run, which no part of
// SA1026 reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
var upstreamPackages = []string{"go1.0/CheckUnsupportedMarshal", "go1.18/CheckUnsupportedMarshal"}

// staticcheck's fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixture and parity's, the second holding the values and types the encoders' walks judge in more
// than one way, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1026.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstreamPackages), "parity")...)
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck and records no
// difference, since there is none, so the two find the same findings there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1026.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
