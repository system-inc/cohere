package range_over_rune_conversion

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa6003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixtures for SA6003, copied unchanged from its testdata/go1.0 and testdata/go1.18,
// each into testdata/src under its version folder, since both are the folder CheckRangeStringRunes. The version folder
// names no language version here: the fixtures load with the toolchain's, which the generic one needs and the other
// does not mind.
var upstream = []string{"go1.0/CheckRangeStringRunes", "go1.18/CheckRangeStringRunes"}

// staticcheck's own fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream...)
}

// Over staticcheck's fixtures and parity's, the second holding the shapes the check depends on, the original and the
// port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6003.Analyzer, Analyzer, analysistest.TestData(),
		append(slices.Clone(upstream), "parity")...)
}

// The differences fixture holds the cases where the two could plausibly disagree, and records no difference, since
// the two read the same form and differ nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6003.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
