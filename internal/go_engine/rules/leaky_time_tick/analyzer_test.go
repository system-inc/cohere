package leaky_time_tick

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1015"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// staticcheck's own fixtures for SA1015, copied from staticcheck/sa1015/testdata/<version> into testdata/<version>,
// hold the port to every `//@ diag(...)` note the original expects, and to nothing more. Each version's folder holds
// a go.mod naming that version, the one staticcheck's testutil lays over the folder as it runs it, since the version
// decides the check: from Go 1.23 an unreferenced ticker is collected, so the one of Go 1.23 marks nothing, and the
// main package of Go 1.0 marks nothing by design. One upstream file is not as staticcheck ships it: gofmt removed a
// second blank line before `type T` in go1.0/CheckLeakyTimeTick, since the repository's gofmt guard walks testdata,
// and the line it removed is after every mark.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), "go1.0", "go1.23")
}

// Over staticcheck's fixtures and parity's, the original and the port find the same findings. The parity tree, of
// packages with no version, holds the shapes the check depends on, commands, a program and test files, and three
// modules hold the versions the check reads: Go 1.20, whose tagged files have their tag's library, older or newer,
// Go 1.22, whose files tagged newer have their tag's, and Go 1.23.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1015.Analyzer, Analyzer, analysistest.TestData(), "go1.0", "go1.23", "parity/...",
		"parity_go1.20", "parity_go1.22", "parity_go1.23")
}

// The differences fixture holds the cases where the two could plausibly disagree. It records nothing, because the
// two read the same form and differ nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1015.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
