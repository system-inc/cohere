package sort_helper_call

import (
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1032"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1032, copied unchanged from simple/s1032/testdata/go1.0/
// CheckSortHelpers into testdata/src/CheckSortHelpers. The go1.0 folder sets the language version of staticcheck's
// run, which S1032 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
// It holds no golden file, since S1032 suggests no fix.
const upstream = "CheckSortHelpers"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	fixture := filepath.Join(analysistest.TestData(), "src", upstream)
	wants, readError := staticcheck_wants.Read(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	_, port := parity.Findings(test, s1032.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its path below testdata's src.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(each.File)), Line: each.Line,
			Message: each.Message,
		})
	}
	test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// Over staticcheck's fixture and parity's, the second holding every shape the check branches on and the near miss of
// each, a renamed import of package sort and a generated file, the original and the port find the same multiset, each
// finding the same to its end, which is the whole call of sort.Sort. S1032 suggests no fix, so a fix either side made
// would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1032.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds every conversion to a helper the original misses for its spelling, through
// parentheses, a local alias, another package's alias and a dot import, and the plainly spelled calls the original
// silences beside each, all recorded `// port finds:` on their lines, and the original finds nothing there the port
// does not. Its nested package, differences/aliased, only declares the alias it converts to, finds nothing and records
// nothing, so it is no differences package of its own.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1032.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
