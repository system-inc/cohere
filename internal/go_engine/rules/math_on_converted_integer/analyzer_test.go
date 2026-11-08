package math_on_converted_integer

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4015"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream are staticcheck's own fixtures for SA4015, copied unchanged from staticcheck/sa4015/testdata into
// testdata/src, each under the folder of the Go version staticcheck runs it at. The two hold one package path, so the
// folder is kept as the first element of each import path. The version folders set the language version of
// staticcheck's run, which no part of SA4015 reads; the go1.18 fixture's type parameters need 1.18, and a fixture tree
// loaded without a module is read at the toolchain's own version.
var upstream = []string{"go1.0/CheckMathInt", "go1.18/CheckMathInt"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// function the original judges given values converted from each kind of number, integers named and not, type
// parameters whose type sets hold integers alone and more, constants and values not converted, and the shapes
// staticcheck's representation builds the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4015.Analyzer, Analyzer, analysistest.TestData(), append(upstream, "parity")...)
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4015.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
