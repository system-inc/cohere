package struct_tag

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/structtag"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for structtag, a and the a/b it imports, copied unchanged into testdata/src, hold the port to
// every want the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and the shapes most likely to
// differ, the original and the port find the same findings, each the same in every field: embedding through aliases,
// generics, pointers, other files, other packages and a dot import, struct types in functions, closures and
// constraints, a package under encoding/ and one that only looks it. The port differs from the original nowhere, so
// there is no differences fixture.
//
// No finding depends on the order the loader gives a package's files: each struct remembers only the names its own
// fields claim, in the order the struct declares them, wherever the embedded types are declared.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(
		test, structtag.Analyzer, Analyzer, analysistest.TestData(),
		"a", "parity", "parity/inner", "sibling", "encoding/skipped", "encodingish/kept",
	)
}
