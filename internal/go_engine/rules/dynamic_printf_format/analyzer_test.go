package dynamic_printf_format

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1006"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1006, copied unchanged from
// staticcheck/sa1006/testdata/go1.0/CheckUnsafePrintf into testdata/src/CheckUnsafePrintf with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which SA1006 never reads, so the fixture loads from a
// tree laid out by import path as the house's fixtures do.
const upstream = "CheckUnsafePrintf"

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
// holds every function the original names, called directly, through promoted and interface methods, through selectors
// of fields and calls and through parentheses, formats of every shape and the calls the original leaves alone, and a
// generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1006.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, and records none, since the port
// parts from the original nowhere, so the two must agree there finding for finding, fixes included. It holds the
// original's slips, a constant format, a parenthesized callee's fix and fmt.Errorf's fix, and the shapes a pattern and
// a walk might read apart, a dot import, an embedded testing.TB and a method expression.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1006.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
