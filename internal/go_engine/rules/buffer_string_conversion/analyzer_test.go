package buffer_string_conversion

import (
	"testing"

	"honnef.co/go/tools/simple/s1030"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstreams are staticcheck's own fixtures for S1030, each copied unchanged, golden file and all, from
// simple/s1030/testdata/go1.0/CheckBytesBufferConversions and go1.9/CheckBytesBufferConversions into testdata/src
// under the same two folders, since the two fixtures share one folder name. The go1.N folder sets the language version
// of staticcheck's run, which S1030 never reads, gating nothing, so the fixtures load from a tree laid out by import
// path as the house's fixtures do, where the aliases of the go1.9 one type-check under the newest language.
var upstreams = []string{"go1.0/CheckBytesBufferConversions", "go1.9/CheckBytesBufferConversions"}

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's two; parity, which holds every shape the query and the judgment branch on and the near miss of each,
// and a generated file, which both filter; parity/shadowed, which declares byte and string at package scope; and
// bytes_test, the path of bytes' external tests, which both skip.
var fixtures = []string{
	"go1.0/CheckBytesBufferConversions", "go1.9/CheckBytesBufferConversions", "parity", "parity/shadowed",
	"bytes_test",
}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any name; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1030.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1030.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The port's fixes, applied to each upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. Each finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreams...)
}

// Over staticcheck's fixtures and the parity fixtures, the original and the port find the same multiset, each
// finding the same to its end and its fixes. The fix's text is the one the original builds through
// edit.ReplaceWithPattern and the port through edit.ReplaceWithNode, so this is where the two are held byte for byte,
// a receiver in parentheses, across lines and with comments beside it included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1030.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The differences fixture holds the shapes where the two could most plausibly part, holders that embed a buffer
// beside methods of its names, receivers across lines and with comments beside them, and conversions nested in
// conversions, calls and function literals. The port's findings there are the original's, to their ends and fixes,
// but where a record says the port drops one, a conversion whose receiver's other method, the one the message asks
// for, is not the buffer's own.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1030.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
