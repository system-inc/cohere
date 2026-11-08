package duplicate_trim_character

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1024"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1024, copied unchanged from staticcheck/sa1024/testdata/go1.0/CheckNonUniqueCutset
// into testdata/src/CheckNonUniqueCutset, holds the port to every `//@ diag(...)` note the original expects, and to
// nothing more. The go1.0 folder sets the language version of staticcheck's run, which no part of SA1024 reads, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckNonUniqueCutset")
}

// Over staticcheck's fixture and parity's, the second holding the shapes staticcheck's representation builds a cutset
// or the call in more than one way, and the cutsets of runes that repeat and of runes that do not, the original and
// the port find the same findings.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1024.Analyzer, Analyzer, analysistest.TestData(), "CheckNonUniqueCutset", "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck and records no
// difference, since there is none, so the two find the same findings there.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1024.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
