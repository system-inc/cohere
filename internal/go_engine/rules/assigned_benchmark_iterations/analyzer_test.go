package assigned_benchmark_iterations

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa3001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA3001, copied unchanged from
// staticcheck/sa3001/testdata/go1.0/CheckBenchmarkN into testdata/src/CheckBenchmarkN. The go1.0 folder sets the
// language version of staticcheck's run, which SA3001 never reads, so the fixture loads from a tree laid out by import
// path as the house's fixtures do.
const upstream = "CheckBenchmarkN"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, the rendered selector
// included. parity holds every shape the pattern accepts or refuses, the operators and side counts of an assignment,
// parentheses, the operand types a selector named N can have and the fields of other types named N.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa3001.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the assignments where the port's test of syntax could most plausibly part from the
// pattern, parentheses, an alias, an embedding, a call as the operand and more than one value. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa3001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
