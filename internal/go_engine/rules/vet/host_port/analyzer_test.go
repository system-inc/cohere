package host_port

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/hostport"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixture for hostport, a.go and its golden a.go.golden, copied unchanged into testdata/src and run as
// x/tools' test runs it, with suggested fixes and no flags: every `// want` the original expects, the port reports at
// the same line with the same message, analysistest fails on a finding no want expects as much as on a want nothing
// found, so the port adds nothing there either, and every fix the port offers, applied together, turns a.go into the
// golden.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixture and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field, its suggested fixes among
// them. The port differs from the original nowhere, so there is no differences fixture.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, hostport.Analyzer, Analyzer, analysistest.TestData(), "a", "parity", "parity/dotted", "parity/indirect",
	)
}
