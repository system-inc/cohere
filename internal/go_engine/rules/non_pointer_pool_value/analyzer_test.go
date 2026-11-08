package non_pointer_pool_value

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa6002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA6002, copied unchanged from
// staticcheck/sa6002/testdata/go1.0/CheckSyncPoolValue into testdata/src/CheckSyncPoolValue. The go1.0 folder sets the
// language version of staticcheck's run, which no part of SA6002 reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
const upstream = "CheckSyncPoolValue"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds values of
// each kind of type, pointers, slices, maps, channels, functions, interfaces, unsafe.Pointer, structs, arrays and
// numbers, named and not, type parameters whose type sets hold a pointer and do not, and the shapes staticcheck's
// representation builds the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6002.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck, and records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa6002.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
