package unencodable_binary_value

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA1003, one for each Go version its run sets, copied unchanged from
// staticcheck/sa1003/testdata/<version>/CheckEncodingBinary into testdata/<version>/CheckEncodingBinary, hold the port
// to every `//@ diag(...)` note the original expects, and to nothing more. Each version's folder holds a go.mod naming
// that version, the one staticcheck's testutil lays over the folder as it runs it, since the version decides whether a
// bool is encodable: Go 1.7's standard library could not encode one, and Go 1.8's can.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.7", "go1.8")
}

// Over staticcheck's fixtures and the house's two parity modules, the original and the port find the same findings.
// parity is older than Go 1.21 and parity_current newer, each with files tagged older and newer than its module, and
// they hold the types of every kind binary.Write is given and the shapes staticcheck's representation builds an
// argument or a call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1003.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.7", "go1.8",
		"parity", "parity_current")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1003.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The package_initializers fixture holds binary.Write in package-level variables' initializers, where the original
// panics on a value whose type holds a bool: it asks the standard library's version of the package initializer's
// file, which there is none of, and fails with "no file found for node" in staticcheck's code.StdlibVersion. So the
// port must find there exactly what the fixture records, each on its line, the version read from the file of each
// call.
func TestPortJudgesWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa1003.Analyzer, Analyzer, analysistest.TestData(),
		"package_initializers")
}
