package double_negation

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4013"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4013, copied unchanged from
// staticcheck/sa4013/testdata/go1.0/CheckDoubleNegation into testdata/src/CheckDoubleNegation with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which SA4013 never reads, so the fixture loads from a
// tree laid out by import path as the house's fixtures do.
const upstream = "CheckDoubleNegation"

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
// holds every way the pattern accepts or refuses a negation: parentheses around either negation and the operand, runs
// of three and four negations, each pair a finding, operands of every kind, a type parameter's, the original's fix that
// drops the parentheses an operand stood in, other unary operators and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4013.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the fixes' printing could part, a negation spread over lines, beside
// comments and inside parentheses, around composite operands. It records no difference, since there is none, so the
// two must agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4013.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
