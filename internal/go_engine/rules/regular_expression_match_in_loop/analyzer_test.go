package regular_expression_match_in_loop

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa6000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA6000, copied unchanged from
// staticcheck/sa6000/testdata/go1.0/CheckRegexpMatchLoop into testdata/src/CheckRegexpMatchLoop. The go1.0 folder sets
// the language version of staticcheck's run, which no part of SA6000 reads, so the fixture loads from a tree laid out
// by import path as the house's fixtures do.
const upstream = "CheckRegexpMatchLoop"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds loops of
// each shape, for, range, labelled, nested, a goto back, a closure inside a loop, and blocks after a loop, a pattern
// constant and not, and the shapes staticcheck's representation builds the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6000.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck, and records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
