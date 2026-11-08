package shell_command_as_program

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1005, copied unchanged from staticcheck/sa1005/testdata/go1.0/CheckExec
// into testdata/src/CheckExec. The go1.0 folder sets the language version of staticcheck's run, which SA1005 never
// reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckExec"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, and neither offers a
// fix. parity holds every way the query accepts or refuses a call: the callee through parentheses, a dot and a
// renamed import, a method value and functions named Command that are not os/exec's, and the program as a constant
// written in each way, a typed and a named one, one inside parentheses, one with a space and a slash or a backslash
// and one without a space, and as values that are not constant, in a generated file too.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1005.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity/dotted")
}

// The differences fixture holds the shapes where the range could part, a program spread over lines, beside comments,
// inside parentheses and joined from constants. It records no difference, since there is none, so the two must agree
// there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1005.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
