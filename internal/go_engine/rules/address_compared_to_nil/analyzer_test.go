package address_compared_to_nil

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4022"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4022, copied unchanged from staticcheck/sa4022/testdata/go1.0/CheckAddressIsNil
// into testdata/src/CheckAddressIsNil, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which SA4022 never reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckAddressIsNil")
}

// Over staticcheck's fixture, parity's and shadowed's, the original and the port find the same findings, the whole
// expression's range held as well as its start. parity holds the address of every kind of operand compared with nil by
// == and by !=, parentheses around the address, its operand or nil, the comparisons the pattern refuses for their
// order, their operator or a nil that is no builtin, a type parameter's variable and a generated file; shadowed
// declares its own nil at package scope, so no comparison in it is with the builtin.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4022.Analyzer, Analyzer, analysistest.TestData(), "CheckAddressIsNil", "parity",
		"shadowed")
}

// The differences fixture holds the comparisons where the two could most plausibly part, an address that can never be
// nil written through a dereference or a conversion, comparisons nested in others and negated, and a switch on an
// address with a nil case, which is no binary expression. It records no difference, since the port parts from the
// original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4022.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
