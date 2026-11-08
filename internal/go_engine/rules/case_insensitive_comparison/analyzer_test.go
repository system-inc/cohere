package case_insensitive_comparison

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa6005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA6005, copied unchanged from
// staticcheck/sa6005/testdata/go1.0/CheckToLowerToUpperComparison into testdata/src/CheckToLowerToUpperComparison with
// its golden file. The go1.0 folder sets the language version of staticcheck's run, which SA6005 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckToLowerToUpperComparison"

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

// Over staticcheck's fixture and parity's, the original and the port find the same findings, fixes included. parity
// holds every way the pattern accepts or refuses a comparison: the operators, the functions on each side and whether
// they agree, parentheses around the calls, their functions and their arguments, functions held in variables or of
// the bytes package, and the strings package imported under another name and into a file's own scope.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6005.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the comparisons where the port's reading or the fix it prints could part from the
// pattern's, parenthesized arguments, a parenthesized function on one side, a comparison over two lines and calls as
// arguments. It records no difference, since there is none, so the two must agree there finding for finding, fixes
// included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6005.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
