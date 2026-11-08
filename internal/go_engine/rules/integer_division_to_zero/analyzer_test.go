package integer_division_to_zero

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4025"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4025, copied unchanged from
// staticcheck/sa4025/testdata/go1.0/CheckIntegerDivisionEqualsZero into testdata/src/CheckIntegerDivisionEqualsZero,
// holds the port to every `//@ diag(...)` note the original expects, and to nothing more. The go1.0 folder sets the
// language version of staticcheck's run, which SA4025 never reads, so the fixture loads from a tree laid out by
// import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckIntegerDivisionEqualsZero")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, so the rendered division
// in each message is held as well as its range. parity holds divisions of two integer literals in every spelling,
// under signs and parentheses, landing in integer, float and named float types, in constants, an array length, a
// conversion, an argument and a return, the divisions the pattern refuses for a named constant, a float or character
// literal, a conversion or arithmetic on either side, those that truncate to anything but zero or below it, a type
// parameter's and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4025.Analyzer, Analyzer, analysistest.TestData(),
		"CheckIntegerDivisionEqualsZero", "parity")
}

// The differences fixture holds the divisions where the two could most plausibly part, literals larger than any
// integer type, results landing in a complex or an unsigned type, a division in an array length, and divisions split
// across lines or holding a comment, which Render could print differently from the source. It records no difference,
// since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4025.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
