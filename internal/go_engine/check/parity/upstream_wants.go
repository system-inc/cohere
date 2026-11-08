package parity

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// RequireUpstreamWants runs port over the original's own fixtures in testdata, copied unchanged: every `// want` the
// original expects, the port reports at the same line with the same message, and analysistest fails on a finding no
// want expects as much as on a want nothing found, so the port adds nothing there either.
//
// It reads x/tools' wants, the `// want` comments analysistest reads. An original whose fixtures mark their findings
// another way gets a function of its own beside this one, in a file of its own, named for the source it reads and
// taking the same arguments, so a port's test reads the same whichever original it ports. staticcheck's fixtures mark
// theirs with `//@ diag(...)` notes, and RequireStaticcheckWants, reading them, is to sit here once its reader lands.
func RequireUpstreamWants(test testing.TB, port *analysis.Analyzer, testdata string, patterns ...string) {
	test.Helper()
	analysistest.Run(errorsOnly{test}, testdata, port, patterns...)
}
