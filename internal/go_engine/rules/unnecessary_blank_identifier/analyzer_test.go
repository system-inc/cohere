package unnecessary_blank_identifier

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
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

// goldens are the upstream files with a golden file beside them, by the version of the module that holds them.
var goldens = []struct {
	version string
	file    string
}{
	{version: "go1.0", file: "LintBlankOK.go"},
	{version: "go1.0", file: "receive-blank.go"},
	{version: "go1.4", file: "range.go"},
}

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

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.3 fixture holds no note, so there
// the original must report nothing too, as staticcheck's own test requires of it, and the ranges it holds are the ones
// the go1.4 fixture's notes expect, so the gate is what parts them.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wanted := 0
	for _, version := range versions {
		fixture := filepath.Join(module(version), upstream)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		wanted += len(wants)
		original, port := parity.Findings(test, s1005.Analyzer, Analyzer, module(version), "./"+upstream)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its path below its module's folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(module(version), filepath.FromSlash(each.File)), Line: each.Line,
				Message: each.Message,
			})
		}
		test.Logf("under %s the fixture expects %d, the original found %d, the port %d", version, len(wants),
			len(original), len(found))
		if len(wants) == 0 && len(original) != 0 {
			test.Errorf("under %s the fixture expects nothing, and the original found %v, so the version did not "+
				"reach it as staticcheck's testutil hands it over", version, original)
		}
		unexpected, untaken := staticcheck_wants.Compare(wants, found)
		for _, extra := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
				extra.Message)
		}
		for _, missing := range untaken {
			test.Errorf("the port reported nothing matching %s", missing)
		}
	}
	if wanted == 0 {
		test.Fatal("the upstream fixtures hold no notes, so the comparison proves nothing")
	}
}

// The port's fixes, applied to each upstream file that has a golden file, make it, as staticcheck's testutil requires
// of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when it holds
// no sections, and the fixed file is gofmt'd before it is compared. The go1.3 fixture has no golden file, and no fix.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, golden := range goldens {
		_, port := parity.Findings(test, s1005.Analyzer, Analyzer, module(golden.version), "./"+upstream)
		path := filepath.Join(module(golden.version), upstream, golden.file)
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		goldenSource, goldenError := os.ReadFile(path + ".golden")
		if goldenError != nil {
			test.Fatal(goldenError)
		}
		archive := txtar.Parse(goldenSource)
		if len(archive.Files) > 0 {
			test.Fatalf("%s's golden file holds %d sections, one a fix's message, which this test does not read",
				golden.file, len(archive.Files))
		}
		edits := editsIn(port, upstream+"/"+golden.file)
		test.Logf("under %s in %s the port made %d edits", golden.version, golden.file, len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", golden.file)
		}
		formatted, formatError := format.Source(applied(test, golden.file, source, edits))
		if formatError != nil {
			test.Fatalf("the port's fixes to %s make source gofmt cannot format: %v", golden.file, formatError)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes to %s make\n%s\nand the golden file is\n%s", golden.file, formatted,
				archive.Comment)
		}
	}
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
// the original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two
// are required to agree there finding for finding, fixes and all, which fails when the original finds nothing, so the
// agreement is over findings.
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
