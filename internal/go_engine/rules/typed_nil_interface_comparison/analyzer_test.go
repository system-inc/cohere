package typed_nil_interface_comparison

import (
	"slices"
	"testing"

	"honnef.co/go/tools/staticcheck/sa4023"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreamPackages are staticcheck's own fixtures for SA4023, copied unchanged from its testdata into testdata/src,
// each under its Go version folder, which stays the first element of its import path since three of them are packages
// named CheckTypedNilInterface. SA4023 reads no Go version, so the fixtures load from one tree laid out by import path.
var upstreamPackages = []string{
	"go1.0/CheckTypedNilInterface",
	"go1.0/i26000",
	"go1.0/i27815",
	"go1.0/i28241",
	"go1.0/i31873",
	"go1.0/i33965",
	"go1.0/i33994",
	"go1.0/i35217",
	"go1.9/CheckTypedNilInterface",
	"go1.18/CheckTypedNilInterface",
}

// staticcheck's fixtures hold the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreamPackages...)
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds results of
// functions in its own package and in parity/producer, whose nilness facts cross the package boundary, by every
// position up to the twelfth, methods, function values, closures and interface methods, interface values made from
// pointers, numbers, constants and errors, == and !=, nil on the left, switches, phis, generic code, a test file and a
// generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4023.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, []string{"parity", "parity/producer"})...)
}

// The differences fixture holds the comparisons where the two could most plausibly part. It records no difference,
// since the two read the same form and part nowhere, so they must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4023.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// A callee declared in a package the judged package never imports is judged as any other package's function is: not
// trivial, unread, so the comparison is reported. indirect imports mid alone, and mid.New returns a pointer to deep's
// Core, whose Err it compares with nil. The original asks its representation for Err's function, is handed none, and
// passes nil to irutil.IsTrivial, which dereferences it: the panic was seen at irutil's stub.go:49, called from
// sa4023.go:192. So over the indirect fixture the original must panic, and the port must find exactly what the
// fixture records with `// port finds:`, the comparison, which is always true.
func TestPortReportsACalleeOfAPackageNeverImportedWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequirePortWhereOriginalPanics(test, sa4023.Analyzer, Analyzer, analysistest.TestData(), "indirect")
}
