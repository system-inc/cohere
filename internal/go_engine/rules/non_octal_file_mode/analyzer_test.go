package non_octal_file_mode

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA9002, copied unchanged from
// staticcheck/sa9002/testdata/go1.0/CheckNonOctalFileMode into testdata/src/CheckNonOctalFileMode with its golden file.
// The go1.0 folder sets the language version of staticcheck's run, which SA9002 never reads, so the fixture loads from
// a tree laid out by import path as the house's fixtures do.
const upstream = "CheckNonOctalFileMode"

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
// holds literal modes passed to the standard library, to a function of the package's own and through conversions, the
// literals and arguments the original leaves alone, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9002.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part: a mode spread from another call's
// results, a literal inside a nested conversion, a method's argument, a named type over os.FileMode and a generic
// function's argument. It records no difference, since the port parts from the original nowhere, so the two must
// agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9002.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
