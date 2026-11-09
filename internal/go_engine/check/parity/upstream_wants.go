package parity

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// RequireUpstreamWants runs port over the original's own fixtures in testdata, copied unchanged: every `// want` the
// original expects, the port reports at the same line with the same message, and analysistest fails on a finding no
// want expects as much as on a want nothing found, so the port adds nothing there either. A fixture module (see
// fixtureLoad) is run on its own, from its folder, as analysistest runs a testdata that holds a go.mod.
//
// It reads x/tools' wants, the `// want` comments analysistest reads. An original whose fixtures mark their findings
// another way gets a function of its own beside this one, in a file of its own, named for the source it reads and
// taking the same arguments, so a port's test reads the same whichever original it ports, as RequireStaticcheckWants
// reads the `//@ diag(...)` notes staticcheck's fixtures mark theirs with.
func RequireUpstreamWants(test testing.TB, port *analysis.Analyzer, testdata string, patterns ...string) {
	test.Helper()
	requireUpstreamWants(test, analysistest.Run, port, testdata, patterns)
}

// RequireUpstreamWantsWithFixes is RequireUpstreamWants for an original whose own test is
// analysistest.RunWithSuggestedFixes: every want is met as there, and the port's fixes must make the original's
// `.golden` files, copied unchanged beside the fixture files they fix, the forms and conflicts as
// RunWithSuggestedFixes reads them. It holds only a file some fix edits, so a fix in a file with no golden file fails
// on the missing file, and a port that offers no fix at all passes here, where RequireIdentical fails it beside an
// original that offers one.
//
// The golden check formats the fixed file and the golden file before it compares them, so it cannot see an edit's
// stray whitespace. RequireIdentical compares every edit exactly, offsets and text, and holds that, so this adds to
// it and replaces nothing.
func RequireUpstreamWantsWithFixes(test testing.TB, port *analysis.Analyzer, testdata string, patterns ...string) {
	test.Helper()
	requireUpstreamWants(test, analysistest.RunWithSuggestedFixes, port, testdata, patterns)
}

// requireUpstreamWants runs port over each load of the fixtures patterns name with run, analysistest.Run or one that
// checks more beside it, from the load's own folder, reporting through errorsOnly.
func requireUpstreamWants(
	test testing.TB,
	run func(analysistest.Testing, string, *analysis.Analyzer, ...string) []*analysistest.Result,
	port *analysis.Analyzer,
	testdata string,
	patterns []string,
) {
	test.Helper()
	// Held as the machine's own target's call is, since a check analysistest runs may read build.Default.
	defer Target{}.held(test)()
	for _, fixtures := range fixtureLoads(testdata, patterns) {
		run(errorsOnly{test}, fixtures.directory, port, fixtures.patterns...)
	}
}
