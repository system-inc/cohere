package redundant_make_arguments

import (
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1019"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamPackages is staticcheck's own fixtures for S1019, copied unchanged from simple/s1019/testdata/go1.0 and
// testdata/go1.18, each into testdata/src under its version folder, since both are the folder CheckMakeLenCap. The
// version folder sets the language version of staticcheck's run, which S1019 never reads, so the fixtures load from
// a tree laid out by import path, with the toolchain's version, which the generic one needs and the other does not
// mind. They hold no golden file, since S1019 suggests no fix.
var upstreamPackages = []string{"go1.0/CheckMakeLenCap", "go1.18/CheckMakeLenCap"}

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the two queries branch
// on and the near miss of each, a generated file, which both filter, and a map_test.go outside the runtime; shadow,
// which declares a make of its own at package scope; and runtime_test, the package whose map_test.go the original
// skips, with line directives that name that file and another.
var parityPackages = []string{"parity", "shadow", "runtime_test"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under S1018's name, or any other, as readily as under S1019's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1019.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1019.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstreamPackage := range upstreamPackages {
		fixture := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstreamPackage))
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("%s holds no notes, so the comparison proves nothing", upstreamPackage)
		}
		_, port := parity.Findings(test, s1019.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("%s expects %d, the port found %d", upstreamPackage, len(wants), len(found))
		unexpected, untaken := staticcheck_wants.Compare(wants, found)
		for _, extra := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
				extra.Message)
		}
		for _, missing := range untaken {
			test.Errorf("the port reported nothing matching %s", missing)
		}
	}
}

// Over staticcheck's fixtures and the parity fixtures, the original and the port find the same multiset, each finding
// the same to its end, each spanning the argument make defaults with its parentheses removed. S1019 suggests no fix,
// so a fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1019.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, parityPackages)...)
}

// The differences fixture holds the shapes where the two could most plausibly part, an alias and a type declared in a
// function, channels of a type parameter's element, a named constraint whose channels differ in direction, calls
// nested in calls and function literals, a package variable's initializer, a method named make, and sizes read anew
// each time. The port parts from the original in one way, where the original is wrong: it drops a length that
// receives from a channel, each such finding recorded on its line as `// port drops: <the original's message>`. Every
// other finding the two make there is the same to its end.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1019.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
