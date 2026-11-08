package invalid_string_conversion_argument

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1030"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA1030, copied unchanged from staticcheck/sa1030/testdata into testdata/src, each
// under the folder of the Go version staticcheck runs it at, hold the port to every `//@ diag(...)` note the original
// expects, and to nothing more. The two hold one package path and one function name, so the folder is kept as the
// first element of each import path. The files are unchanged but for the one line gofmt adds to go1.15's
// CheckStrconv.go, a `//go:build` line above its `// +build` line, since the repository's gofmt guard reads testdata
// too; every note is read from the copy, so none moves off its line. The version folders set the language version of
// staticcheck's run, which no part of SA1030 reads; the go1.15 fixture's build constraint holds for every toolchain
// that builds cohere.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0/CheckStrconv", "go1.15/CheckStrconv")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. parity holds every
// function the original judges called with each bit size, base and format at and around the bounds it accepts,
// constants written in each way and values that are not constant, and the shapes staticcheck's representation builds
// the call in more than one way.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1030.Analyzer, Analyzer, analysistest.TestData(), "go1.0/CheckStrconv",
		"go1.15/CheckStrconv", "parity")
}

// The differences fixture holds the calls where staticcheck_call takes another path than callcheck. It records no
// difference, since there is none, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1030.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
