package repeated_if_else_condition

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4014"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4014, copied unchanged from
// staticcheck/sa4014/testdata/go1.0/CheckRepeatedIfElse into testdata/src/CheckRepeatedIfElse. The go1.0 folder sets
// the language version of staticcheck's run, which SA4014 never reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
const upstream = "CheckRepeatedIfElse"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds repeats of
// every kind of condition, a condition tested three and four times, chains an initializer or a side effect stops part
// of the way, chains nested in a branch of another, conditions that render alike, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4014.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the chains where the two could most plausibly part, conditions that differ only in
// parentheses or a line break, which both compare by staticcheck's Render, a repeat after an initializer stops the
// chain, closures called in conditions, and conditions that are the same but for order. It records no difference,
// since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4014.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
