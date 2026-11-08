package reversed_errors_is_arguments

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1032"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1032, copied unchanged from
// staticcheck/sa1032/testdata/go1.0/example.com/ErrorsOrder into testdata/src/example.com/ErrorsOrder, holds the port
// to every `//@ diag(...)` note the original expects, and to nothing more. The go1.0 folder sets the language version
// of staticcheck's run, which no part of SA1032 reads, so the fixture loads from a tree laid out by import path as the
// house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "example.com/ErrorsOrder")
}

// Over staticcheck's own fixture and parity's, the original and the port find the same findings. parity holds errors
// of each origin on each side, globals of the standard library, of another fixture package and of its own, locals,
// values built at the call and globals read through a phi or a pointer, and the shapes staticcheck's representation
// builds the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1032.Analyzer, Analyzer, analysistest.TestData(), "example.com/ErrorsOrder",
		"parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck and records no
// difference, since there is none, so the two must find the same findings there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1032.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
