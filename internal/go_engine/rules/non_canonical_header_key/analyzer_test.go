package non_canonical_header_key

import (
	"testing"

	"honnef.co/go/tools/staticcheck/sa1008"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for SA1008, copied unchanged from
// staticcheck/sa1008/testdata/go1.0/CheckCanonicalHeaderKey into testdata/src/CheckCanonicalHeaderKey with its golden
// file. The go1.0 folder sets the language version of staticcheck's run, which SA1008 never reads, so the fixture loads
// from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckCanonicalHeaderKey"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to staticcheck's fixture, make its golden file, as staticcheck's testutil requires of the
// original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity and parity/keys, the original and the port find the same findings, fixes
// included. parity holds every way the original reads or skips a key: literals raw and interpreted, local, package and
// qualified constants, a parenthesized literal and a concatenation, which take no fix, keys already canonical or not
// constant, keys http.CanonicalHeaderKey leaves alone, reads in definitions, ranges and calls, writes to a Header whose
// right side goes unjudged, the original's miss, and writes to a plain map whose right side is judged, a parenthesized
// write, an alias, an alias of an alias, a named type of its own, a dereferenced pointer, a field, a slice element,
// conversions, type parameters, nested indexes, a key beside comments, a file importing net/http under another name,
// whose fix still says http, and a generated file, which the original does not filter.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1008.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity",
		"parity/keys")
}

// The differences fixture holds the shapes where the fixes could part, keys spread over lines and beside comments, raw
// and escaped literals and a shadowed constant, and records no difference, since there is none, so the two must agree
// there finding for finding, fixes included.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa1008.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
