package negative_length_comparison

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4024"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4024, copied unchanged from
// staticcheck/sa4024/testdata/go1.0/CheckBuiltinZeroComparison into testdata/src/CheckBuiltinZeroComparison, holds
// the port to every `//@ diag(...)` note the original expects, and to nothing more. The go1.0 folder sets the
// language version of staticcheck's run, which SA4024 never reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckBuiltinZeroComparison")
}

// Over staticcheck's fixture, parity's and shadowed's, the original and the port find the same findings. parity holds
// len and cap of every kind of operand compared below zero in both orders the pattern names, zero in every spelling
// an integer literal takes, parentheses around the call, its function and the zero, the comparisons the pattern
// refuses for their order, their operator, a zero that is no literal, a function that is no builtin or a len a block
// declares, a type parameter's operands and a generated file; shadowed declares its own len and cap at package scope,
// so no call in it is of the builtins.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4024.Analyzer, Analyzer, analysistest.TestData(),
		"CheckBuiltinZeroComparison", "parity", "shadowed")
}

// The differences fixture holds the comparisons where the two could most plausibly part, a len that is a constant, a
// builtin in parentheses, a zero of an unusual spelling, both orders in one condition, and operands of a type
// parameter, a pointer to an array and a composite literal. It records no difference, since the port parts from the
// original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4024.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
