package unexiting_test_main

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa3000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixtures for SA3000, one for each Go version its run sets, copied unchanged from
// staticcheck/sa3000/testdata/<version>/<Package> into testdata/<version>/<Package>. Each version's folder holds a
// go.mod naming that version, the one staticcheck's testutil lays over the folder as it runs it, since the version
// decides whether a TestMain must exit: Go 1.15's test framework exits for it, and Go 1.4's does not. Each folder of
// testdata named here holds a go.mod, so each loads as a module of its own.
var upstream = []string{"go1.4", "go1.15"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream...)
}

// Over staticcheck's fixtures and the house's two parity modules, one older than Go 1.15 with files tagged older and
// newer than it, and one of Go 1.22 with a file tagged older, the first holding every shape of TestMain, call to
// os.Exit and call to Run the check tells apart, the original and the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa3000.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstream), "parity", "parity_current")...)
}

// The differences fixture holds the TestMains whose judgment hangs on the state the walk carries, a call to os.Exit at
// package level and calls inside a call's arguments. It records no difference, since there is none, so the two must
// agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa3000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
