package struct_conversion_by_field_copy

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
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

// upstreamFiles is the one file of the upstream fixture under each version, the one its golden file fixes.
var upstreamFiles = map[string]string{
	"go1.0":  "convert.go",
	"go1.7":  "convert.go",
	"go1.8":  "convert.go",
	"go1.9":  "convert_alias.go",
	"go1.18": "convert_generics.go",
}

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
		fixture := filepath.Join(module(version), upstream)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("under %s the upstream fixture holds no notes, so the comparison proves nothing", version)
		}
		_, port := parity.Findings(test, s1016.Analyzer, Analyzer, module(version), "./"+upstream)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its path below its module's folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(module(version), filepath.FromSlash(each.File)), Line: each.Line,
				Message: each.Message,
			})
		}
		test.Logf("under %s the fixture expects %d, the port found %d", version, len(wants), len(found))
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

// The port's fixes, applied to each version's upstream fixture, make its golden file, as staticcheck's testutil
// requires of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when
// it holds no sections, and the fixed file is gofmt'd before it is compared.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		_, port := parity.Findings(test, s1016.Analyzer, Analyzer, module(version), "./"+upstream)
		path := filepath.Join(module(version), upstream, upstreamFiles[version])
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		golden, goldenError := os.ReadFile(path + ".golden")
		if goldenError != nil {
			test.Fatal(goldenError)
		}
		archive := txtar.Parse(golden)
		if len(archive.Files) > 0 {
			test.Fatalf("under %s the golden file holds %d sections, one a fix's message, which this test does not "+
				"read", version, len(archive.Files))
		}
		edits := editsIn(port, upstream+"/"+upstreamFiles[version])
		test.Logf("under %s the port found %d, with %d edits", version, len(port), len(edits))
		if len(edits) == 0 {
			test.Fatalf("under %s the port made no edit to the upstream fixture, so the comparison proves nothing",
				version)
		}
		fixed := applied(test, upstreamFiles[version], source, edits)
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("under %s the port's fixes make source gofmt cannot format: %v\n%s", version, formatError,
				fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("under %s the port's fixes make\n%s\nand the golden file is\n%s", version, formatted,
				archive.Comment)
		}
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

// editsIn is every edit of every fix of findings in the file named file, by its path below its module's folder.
func editsIn(findings []parity.Finding, file string) []parity.Edit {
	edits := []parity.Edit{}
	for _, finding := range findings {
		for _, fix := range finding.Fixes {
			for _, edit := range fix.Edits {
				if edit.File == file {
					edits = append(edits, edit)
				}
			}
		}
	}
	return edits
}

// applied is source with edits applied, from the last to the first, so each edit's offsets still name the bytes they
// were read against. Two edits that overlap fail the test.
func applied(test *testing.T, file string, source []byte, edits []parity.Edit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(first parity.Edit, second parity.Edit) int {
		return cmp.Compare(second.Start, first.Start)
	})
	fixed := slices.Clone(source)
	for index, edit := range ordered {
		if index > 0 && edit.End > ordered[index-1].Start {
			test.Fatalf("the edits to %s overlap at offset %d", file, edit.Start)
		}
		fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
	}
	return fixed
}
