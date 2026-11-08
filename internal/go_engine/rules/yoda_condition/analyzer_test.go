package yoda_condition

import (
	"testing"

	"honnef.co/go/tools/stylecheck/st1017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for `ST1017`, copied unchanged from stylecheck/st1017/testdata/go1.0 into
// testdata/go1.0, hold the port to every `//@ diag(...)` note the original expects, and to nothing more. The folder
// holds the go.mod staticcheck's testutil lays over it, `module example.com` at Go 1.0.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings, each with the same fix.
// parity holds every comparison shape the check reads, parenthesized, nested and spread over lines, and every near
// miss, a constant that names an identifier among them. The port differs from the original nowhere, so there is no
// differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, st1017.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "parity")
}
