package ineffective_random_integer

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4030"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Staticcheck's own fixtures for SA4030, copied unchanged from its testdata/go1.0 and testdata/go1.22 into
// testdata/src/go1.0 and testdata/src/go1.22, its version folders kept as the first element of each import path, since
// the two packages share a name, hold the port to every note the original expects, and to nothing more. Nothing gates
// SA4030 by version, and the loader type-checks both at the newest version, which the go1.22 fixture's math/rand/v2
// needs.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(),
		"go1.0/CheckIneffectiveRandInt", "go1.22/CheckIneffectiveRandInt")
}

// Over staticcheck's fixtures, parity's, holding the shapes the check depends on and a generated file, which neither
// filters, and dotted's, which calls math/rand's functions by name alone, the original and the port find the same
// findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4030.Analyzer, Analyzer, analysistest.TestData(),
		"go1.0/CheckIneffectiveRandInt", "go1.22/CheckIneffectiveRandInt", "parity", "dotted")
}

// The differences fixture records nothing, because the two read the same form and differ nowhere: it holds the cases
// where they could plausibly disagree, so the test holds that they agree there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4030.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
