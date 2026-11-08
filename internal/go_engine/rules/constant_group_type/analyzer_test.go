package constant_group_type

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa9004"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA9004, copied unchanged from
// staticcheck/sa9004/testdata/go1.0/CheckMissingEnumTypesInDeclaration into
// testdata/src/CheckMissingEnumTypesInDeclaration with its golden file. The go1.0 folder sets the language version of
// staticcheck's run, which SA9004 never reads, so the fixture loads from a tree laid out by import path as the house's
// fixtures do.
const upstream = "CheckMissingEnumTypesInDeclaration"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to staticcheck's fixture, make its golden file, as staticcheck's testutil requires of the
// original's. gofmt, which the harness runs over the fixed file, is what aligns the fixed lines.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the original and the port find the same findings, fixes included. parity
// holds groups of every kind of literal, signed literals, trailing comments, groups split by blank and comment lines, a
// group inside a function, the groups the original leaves alone, and a generated file.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9004.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part: a parenthesized literal, a
// converted literal, a qualified type, comments gofmt aligns, and literals whose type converts to the first's but
// whose value does not fit it. It records no difference, since the port parts from the original nowhere, so the two
// must agree there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa9004.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
