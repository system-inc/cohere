package std_methods

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/stdmethods"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for stdmethods, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
}

// x/tools' second test runs the original over the standard library's encoding/xml, which declares MarshalXML and
// UnmarshalXML methods and interfaces of its own, and expects nothing; the port finds nothing there either. No
// testdata is given, so the package loads from `GOROOT`, as x/tools' test loads it.
func TestPortMatchesUpstreamWantsOverEncodingXml(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, "", "encoding/xml")
}

// Over x/tools' fixtures, parity's and encoding/xml, parity's holding every branch of the judgment and the shapes
// most likely to differ, the original and the port find the same findings, each the same in every field. The
// fixtures load as a tree laid out by import path, so encoding/xml comes from `GOROOT`. The port differs from the
// original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, stdmethods.Analyzer, Analyzer, analysistest.TestData(), "a", "typeparams", "parity", "encoding/xml",
	)
}
