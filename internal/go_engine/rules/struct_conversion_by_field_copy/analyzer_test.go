package struct_conversion_by_field_copy

import (
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1016, which it keeps five times, under simple/s1016/testdata/go1.0,
// go1.7, go1.8, go1.9 and go1.18, each with its golden file, each copied unchanged into the module of its folder's
// name. S1016 asks for identical struct tags where the language version is older than go1.8, so the version matters:
// staticcheck's testutil loads each folder as a module whose go.mod, overlaid, says `go 1.N` for its folder's name,
// and the go line is what its loader and ours alike hand the type checker as the package's version, which the gate
// reads. So each testdata/src/go1.N is a module, example.com as staticcheck's is, whose go.mod says its folder's
// version, which the harness loads as a module since it holds a go.mod, and TestFixtureModulesCarryTheirVersions
// holds that the version reached every package, so a fixture's silence is the gate's and not a version lost.
const upstream = "CheckSimplerStructConversion"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's five.
var versions = []string{"go1.0", "go1.7", "go1.8", "go1.9", "go1.18"}

// parityPackages is the fixtures written for the comparison, by the module each loads from: under go1.18, parity,
// which holds every shape the check branches on and the near miss of each, a renamed import and a generated file, and
// its dependency other; under go1.7, versions, an untagged file the gate holds to identical tags and a tagged file the
// type checker gives go1.21.
var parityPackages = map[string][]string{
	"go1.7":  {"./versions"},
	"go1.18": {"./parity", "./parity/other"},
}

// differencesFixture is the package that records where the port parts from the original, and its dependency box,
// which declares the generic type whose instance two type checkers make. It sits in a tree laid out by import path
// beside the modules, not in one, since the harness reads a fixture's records under testdata's src by the path it
// loads, and no shape the fixture holds turns on the language version.
const differencesFixture = "differences"

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched, as the parity
// harness loads a fixture directory that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under another name as readily as under S1016's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1016.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1016.Analyzer.Name)
	}
}

// Every package of every fixture module carries the version its go.mod says, which is what both analyzers' version
// gates read. The harness loads each module the same way and checks no version, so this keeps a load of its own.
func TestFixtureModulesCarryTheirVersions(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		loaded, loadError := load.Packages(module(version), []string{"./..."}, moduleEnvironment, nil)
		if loadError != nil {
			test.Fatalf("loading the module of %s: %v", version, loadError)
		}
		if len(loaded.Roots) == 0 {
			test.Fatalf("loading the module of %s found no package, so no version was checked", version)
		}
		for _, root := range loaded.Roots {
			if root.Types == nil || root.Types.GoVersion() != version {
				test.Errorf("fixture package %s did not load under %s, which its module says", root.ID, version)
			}
		}
	}
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.7 and go1.8 fixtures hold the
// same literal of a tagged struct, which only the go1.8 note expects, so the gate is what parts them.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		test.Run(version, func(test *testing.T) {
			test.Parallel()
			parity.RequireStaticcheckWants(test, Analyzer, module(version), "./"+upstream)
		})
	}
}

// The port's fixes, applied to each version's upstream fixture, make its golden file, as staticcheck's testutil
// requires of the original's. None of the golden files holds sections, so each is the whole fixed file.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		test.Run(version, func(test *testing.T) {
			test.Parallel()
			parity.RequireStaticcheckGolden(test, Analyzer, module(version), "./"+upstream)
		})
	}
}

// Over each version's upstream fixture and the parity fixtures of its module, the original and the port find the
// same multiset, each finding the same to its end and its fixes. The fix's text is the conversion each builds around
// the literal's own type and the last element's identifier, printed by edit.ReplaceWithNode, so this is where the two
// are held byte for byte, literals across lines with comments among their elements, a renamed import, generic
// instances and aliases among them.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		test.Run(version, func(test *testing.T) {
			test.Parallel()
			parity.RequireIdentical(test, s1016.Analyzer, Analyzer, module(version),
				slices.Concat([]string{"./" + upstream}, parityPackages[version])...)
		})
	}
}

// In the differences fixture, the port's findings are the original's but where a record says the port drops one the
// original makes wrongly: a literal whose address is taken through parentheses, and a literal of the very type its
// fields are copied from, by an alias or by a generic instance another type checker made.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1016.Analyzer, Analyzer, analysistest.TestData(), differencesFixture)
}
