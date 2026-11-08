package unnecessary_sprint

import (
	"testing"

	"honnef.co/go/tools/simple/s1039"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1039, copied unchanged from
// simple/s1039/testdata/go1.0/CheckSprintLiteral into testdata/src/CheckSprintLiteral with its golden file. The go1.0
// folder sets the language version of staticcheck's run, which S1039 never reads, so the fixture loads from a tree
// laid out by import path as the house's fixtures do.
const upstream = "CheckSprintLiteral"

// fixtures is every fixture package, staticcheck's and the port's own: parity's, its dot import, the lookalike
// package spelled fmt at another path, the package at fmt's external tests' path, and the differences, which record
// nothing, since the port parts from the original nowhere, so the original and the port agree in every one.
var fixtures = []string{upstream, "parity", "parity/dotted", "parity/fmt", "fmt_test", "differences"}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity's, which holds every shape the query branches on and the near miss of each, a
// generated file, a dot and a renamed import, a package spelled fmt at another path, the package at fmt's external
// tests' path, and the differences fixture, which holds the shapes where the two could most plausibly part (literals
// whose text is not their value, a conversion to a function type, calls in function and composite literals, and a
// local that takes the name fmt), the original and the port find the same multiset, each finding the same to its end
// and its fixes. Both build the fix's text through edit.ReplaceWithNode from the literal the call is given, so this
// is where the literal each binds is held byte for byte, parenthesized, raw, across lines and among comments
// included. The differences fixture records nothing, which RequireOnlyAdjudicated refuses, so it is held here.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1039.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}
