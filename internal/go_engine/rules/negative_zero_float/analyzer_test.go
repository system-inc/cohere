package negative_zero_float

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4026"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4026, copied unchanged from
// staticcheck/sa4026/testdata/go1.0/CheckNegativeZeroFloat into testdata/src/CheckNegativeZeroFloat with its golden
// file. The go1.0 folder sets the language version of staticcheck's run, which SA4026 never reads, so the fixture loads
// from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckNegativeZeroFloat"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to staticcheck's fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity's and shadowed's, the original and the port find the same findings, fixes
// included. parity holds every way the pattern accepts or refuses a negative zero: -0.0, -float32(0) and float64(-0)
// with parentheses around every part, literals refused for their text, conversions refused for their name, float32 and
// float64 declared as variables, which the original reports as -0.0, a type parameter's and a generated file; shadowed
// declares float64 as an integer type and float32 as its alias at package scope, which the pattern accepts by name.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4026.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity", "shadowed")
}

// The differences fixture holds the shapes where the rendered messages could part, expressions spread over lines,
// beside comments and inside parentheses. It records no difference, since there is none, so the two must agree there
// finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4026.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
