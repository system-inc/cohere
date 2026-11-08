package invalid_template

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixture for SA1001, copied unchanged from staticcheck/sa1001/testdata/go1.0/CheckTemplate into
// testdata/src/CheckTemplate, holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
// The go1.0 folder sets the language version of staticcheck's run, which SA1001 never reads, so the fixture loads from
// a tree laid out by import path as the house's fixtures do.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "CheckTemplate")
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, of which neither offers
// a fix. parity holds every way the query and its filters accept or refuse a call: each package's Parse on a template
// its New makes in the call, and on one made by Must, Delims, Funcs, a variable or a parenthesized New, a method value
// and a method expression, a function named Parse, and the template as a constant written in each way, inside
// parentheses and joined, as a value that is not constant, and each kind of error the template packages give, the
// syntax errors the original reports and the others it leaves, in a generated file too.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1001.Analyzer, Analyzer, analysistest.TestData(), "CheckTemplate", "parity")
}

// The differences fixture holds the shapes where the port's range could part from the original's, a template spread
// over lines, beside comments, inside parentheses and joined from constants. It records no difference, since there is
// none, so the two must agree there finding for finding. The one shape where they part, a parenthesized method, panics
// the original, so it has a fixture and a test of its own.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The original asserts that a Parse call's function is a selector once its query, which looks through parentheses,
// has matched, and panics on `(tt.New("").Parse)(text)`, so it reports nothing for that package, every other call in
// it included. The panics fixture holds that call and a plain one beside it, and the port must make exactly the
// findings it records, both the parenthesized call's and the plain call's, which the original loses with it.
func TestPortReportsWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa1001.Analyzer, Analyzer, analysistest.TestData(), "panics")
}
