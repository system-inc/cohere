package modulo_by_one

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4028"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA4028, copied unchanged from staticcheck/sa4028/testdata/go1.0/CheckModuloOne into
// testdata/src/CheckModuloOne, holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
// The go1.0 folder sets the language version of staticcheck's run, which SA4028 never reads, so the fixture loads from
// a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckModuloOne")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds the divisor
// 1 in every spelling an integer literal takes, under signs and parentheses, the divisors that are 1 only by a named
// constant, a conversion or arithmetic, which the pattern refuses, x %= 1, remainders of a type parameter, nested and
// parenthesized remainders and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4028.Analyzer, Analyzer, analysistest.TestData(), "CheckModuloOne", "parity")
}

// The differences fixture holds the remainders where the two could most plausibly part: a divisor of a type the code
// declares, literals of the largest values their types hold, a remainder folded in a constant or an array length, and
// divisors that are 1 written as a float or a character. It records no difference, since the port parts from the
// original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4028.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
