package unnecessary_blank_identifier

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1005, which it keeps under simple/s1005/testdata/go1.0, go1.3 and go1.4,
// each copied unchanged, golden files and all, into the module of its folder's name. S1005 reports a range statement
// only where the language is go1.4 or newer, so the version matters: staticcheck's testutil loads each folder as a
// module whose go.mod, overlaid, says `go 1.N` for its folder's name, and the go line is what its loader and ours
// alike hand the type checker as the package's version, which the gate reads. So testdata/src/go1.0, go1.3 and go1.4
// are each a module, example.com as staticcheck's is, whose go.mod says its folder's version, which the harness loads
// as a module since the folder holds a go.mod, and TestFixtureModulesLoadUnderTheirVersions checks that the version
// reached every package, so the go1.3 fixture's silence is the gate's and not a version lost.
const upstream = "CheckUnnecessaryBlank"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's three.
var versions = []string{"go1.0", "go1.3", "go1.4"}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// Every package of every fixture module loads under the version its go.mod says, as the harness loads it, since that
// is what both analyzers' version gates read. Were the go line lost, the type checker would hand every package the
// newest language, and the go1.0 and go1.3 fixtures would compare the same findings under the wrong version.
func TestFixtureModulesLoadUnderTheirVersions(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		requireLoadsUnder(test, module(version), version, "./...")
	}
}

// Every `//@ diag(...)` note the upstream fixture holds, under go1.0 and go1.4, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. Each version's module also holds parity's
// fixtures, which carry no note, so each is held to its upstream package alone, one call apiece. The go1.3 fixture
// holds no note, so there the original and the port must both report nothing, as staticcheck's own test requires of
// the original, and the ranges it holds are the ones the go1.4 fixture's notes expect, so the gate is what parts them.
// RequireStaticcheckWants refuses a fixture with no note, since there it would prove nothing on its own, so that
// silence is held here from the findings the harness hands back.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, module("go1.0"), "./"+upstream)
	parity.RequireStaticcheckWants(test, Analyzer, module("go1.4"), "./"+upstream)
	original, port := parity.Findings(test, s1005.Analyzer, Analyzer, module("go1.3"), "./"+upstream)
	test.Logf("under go1.3 the fixture expects nothing, the original found %d, the port %d", len(original), len(port))
	for _, finding := range original {
		test.Errorf("under go1.3 the fixture expects nothing, and the original found %s, so the version did not reach "+
			"it as staticcheck's testutil hands it over", finding)
	}
	for _, finding := range port {
		test.Errorf("under go1.3 the fixture expects nothing, and the port found %s", finding)
	}
}

// The port's fixes, applied to each upstream file that has a golden file, make it, as staticcheck's testutil requires
// of the original's, under go1.0 and go1.4, one call apiece for the same reason as the notes. The go1.3 fixture has no
// golden file, and no fix.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, module("go1.0"), "./"+upstream)
	parity.RequireStaticcheckGolden(test, Analyzer, module("go1.4"), "./"+upstream)
}

// The original and the port run in one checker over staticcheck's fixture and parity's, under go1.4 and go1.3, and
// over staticcheck's under go1.0. Parity's holds every shape the queries and the range branches on and the near miss
// of each, a generated file, and files tagged newer than the module, and the two must find the same multiset, each
// finding the same to its end and its fixes. The receive fixes' text is printed from the syntax each side builds, the
// original through its pattern's binding and its copy of the assignment and the port through its own, so this is
// where the two are held byte for byte, comments inside and beside the assignment and right sides across lines among
// them.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.4"), "./"+upstream, "./parity")
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.3"), "./"+upstream, "./parity")
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.0"), "./"+upstream)
}

// The differences fixture holds the shapes where the two could most plausibly part, parenthesized blanks in a range
// and in an assignment, a definition's two-byte token, a receive-only channel, a labeled receive, both left sides
// blank, and ranges over an alias of a function and over a named map, and records none, since the port parts from
// the original nowhere. RequireOnlyAdjudicated refuses a fixture that records nothing, so the two are required to
// agree there finding for finding, fixes and all.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.4"), "./differences")
}

// Over every fixture of every version, the original suggests both of its fixes, the receive alone's and the rest's,
// which fix differently, so the comparisons above hold each branch's fix and not one alone.
func TestFixturesHoldBothFixes(test *testing.T) {
	test.Parallel()
	original := []parity.Finding{}
	for _, version := range versions {
		originalOfVersion, portOfVersion := parity.Findings(test, s1005.Analyzer, Analyzer, module(version), "./...")
		test.Logf("under %s the original found %d, the port %d", version, len(originalOfVersion),
			len(portOfVersion))
		original = append(original, originalOfVersion...)
	}
	requireBothFixes(test, original)
}

// The shapes gofmt rewrites, spacing inside the deleted ranges, no spacing around an assignment's token, and doubled
// parentheses around a receive and a blank, are held the same way, ends and fixes included. The guard holds every .go
// file of the tree to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go
// into a go1.4 module of the test's own, which the harness loads as a module since it holds a go.mod.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "unformatted", "unformatted.go.txt"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	if writeError := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com\n\ngo 1.4\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	packageDirectory := filepath.Join(directory, "unformatted")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(packageDirectory, "unformatted.go"), source, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	requireLoadsUnder(test, directory, "go1.4", "./unformatted")
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, directory, "./unformatted")
	original, _ := parity.Findings(test, s1005.Analyzer, Analyzer, directory, "./unformatted")
	requireBothFixes(test, original)
}

// requireLoadsUnder fails test unless every package patterns name in the fixture module in directory loads clean
// under version, the go line its go.mod says. The harness loads the module the same way, in module mode with nothing
// fetched, and hands back findings alone, so the version a package loaded under is read here, by a load of its own.
func requireLoadsUnder(test *testing.T, directory string, version string, patterns ...string) {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"},
		nil)
	if loadError != nil {
		test.Fatalf("loading %v under %s: %v", patterns, version, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v under %s found no package, so the check would prove nothing", patterns, version)
	}
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean: %s", failure.Package, failure.Error)
	}
	for _, root := range loaded.Roots {
		if root.Types.GoVersion() != version {
			test.Fatalf("fixture package %s loaded under %q, where its module says %s", root.ID,
				root.Types.GoVersion(), version)
		}
	}
	test.Logf("under %s %d packages loaded under their module's version", version, len(loaded.Roots))
}

// requireBothFixes fails test unless original suggests both of S1005's fix messages, since the receive alone and the
// rest fix differently, and a fixture that holds one branch alone compares only its fix.
func requireBothFixes(test *testing.T, original []parity.Finding) {
	test.Helper()
	messages := map[string]int{}
	for _, finding := range original {
		for _, fix := range finding.Fixes {
			messages[fix.Message]++
		}
	}
	test.Logf("the original's fixes by message: %v", messages)
	for _, message := range []string{"Remove assignment to blank identifier", "Simplify channel receive operation"} {
		if messages[message] == 0 {
			test.Errorf("the original suggested no fix %q, so the fixture does not hold its branch", message)
		}
	}
}
