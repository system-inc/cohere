package wait_group_add_in_goroutine

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa2000"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA2000, copied unchanged from
// staticcheck/sa2000/testdata/go1.0/CheckWaitgroupAdd into testdata/src/CheckWaitgroupAdd. The go1.0 folder sets the
// language version of staticcheck's run, which SA2000 never reads, so the fixture loads from a tree laid out by import
// path as the house's fixtures do.
const upstream = "CheckWaitgroupAdd"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, the rendered call
// included. parity holds every shape the pattern accepts or refuses: where the Add sits in the goroutine's body and
// what wraps it, how the function literal is called, the receivers that reach Add and the Adds of other types.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2000.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the go statements where the port's test of syntax could most plausibly part from the
// pattern, nested blocks, a label over a block, parentheses, a method expression and an embedding. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa2000.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
