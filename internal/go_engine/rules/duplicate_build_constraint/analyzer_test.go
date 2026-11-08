package duplicate_build_constraint

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa4019"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA4019, copied unchanged from
// staticcheck/sa4019/testdata/go1.1/CheckDuplicateBuildConstraints into testdata/src/CheckDuplicateBuildConstraints.
// The go1.1 folder sets the language version of staticcheck's run, which SA4019 never reads, so the fixture loads from
// a tree laid out by import path as the house's fixtures do; its constraints name go1.1, which every release satisfies,
// so the file builds.
const upstream = "CheckDuplicateBuildConstraints"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings. parity holds every way
// the original reads or skips a constraint line: three identical lines, terms repeated a different number of times,
// terms joined by commas in another order, one line repeating a term, block comments in the preamble and as the
// package's doc comment, which the preamble leaves out, fields split by runs of spaces, `+build` with no space or
// nothing after it, a line after the package clause, a file of mixed lines and a generated file, which the original
// filters.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4019.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the range or the message could part, a comment inside the package
// clause and terms holding quotes and backslashes. It records no difference, since there is none, so the two must
// agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4019.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
