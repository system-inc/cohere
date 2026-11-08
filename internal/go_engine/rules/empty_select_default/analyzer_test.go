package empty_select_default

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa5004"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA5004, copied unchanged from staticcheck/sa5004/testdata/go1.0 into
// testdata/src/CheckLoopEmptyDefault with its golden file, holds the port to every `//@ diag(...)` note the original
// expects, and to nothing more. The go1.0 folder sets the language version of staticcheck's run, which SA5004 never
// reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckLoopEmptyDefault")
}

// The port's fixes, applied to staticcheck's fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), "CheckLoopEmptyDefault")
}

// Over staticcheck's fixture and parity's, the second holding every way the pattern accepts or refuses a loop: each of
// a for loop's three clauses, a range loop, a body of one select and of more, the select under a label and in nested
// blocks, and default cases empty, holding a statement or a comment, or missing, the original and the port find the
// same findings, fixes included.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5004.Analyzer, Analyzer, analysistest.TestData(), "CheckLoopEmptyDefault",
		"parity")
}

// The differences fixture holds the loops where the port's reading could part from the pattern, a labeled select in
// a block, and where the deleting fix's range matters, a case beside a comment and one on a single line. It records no
// difference, since there is none, so the two must agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa5004.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
