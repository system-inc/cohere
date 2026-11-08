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
// golden. The harness's RequireUpstreamWants runs without suggested fixes, so it would leave the golden unread, and
// this tier keeps its own call.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	analysistest.RunWithSuggestedFixes(errorsOnly{test}, analysistest.TestData(), Analyzer, "a")
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

// errorsOnly hands analysistest.RunWithSuggestedFixes a reporter that isn't a testing.TB. Given a testing.TB, it first
// proves the go command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test
// that builds. Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
