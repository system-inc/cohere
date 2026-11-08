package time_until

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/simple/s1024"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1024, which it keeps twice, under simple/s1024/testdata/go1.7 and
// go1.8, the second with its golden file, each copied unchanged into the module of its folder's name. S1024 reports
// only where the standard library is go1.8 or newer, so the version matters: staticcheck's testutil loads each folder
// as a module whose go.mod, overlaid, says `go 1.N` for its folder's name, and the go line is what its loader and
// ours alike hand the type checker as the package's version, which the gate reads. So testdata/src/go1.7 and go1.8
// are each a module, example.com as staticcheck's is, whose go.mod says its folder's version, which the harness loads
// as a module since it holds a go.mod, and TestFixtureModulesCarryTheirVersions checks that the version reached every
// package, so the go1.7 fixture's silence is the gate's and not a version lost.
const upstream = "CheckTimeUntil"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's two.
var versions = []string{"go1.7", "go1.8"}

// fixtures is every fixture package of the go1.8 module where the original and the port agree, fixes and all:
// staticcheck's, parity's, and parity's that imports time into its own scope beside its own name.
var fixtures = []string{"./" + upstream, "./parity", "./parity/dotted"}

// differencesFixtures is the differences fixture's packages, where the two make the same findings and the original's
// fixes fail to compile: its own, which imports time as clock alone in one file and hands Sub receivers that are no
// time.Time in the other, and one that imports time into its own scope alone. Its embedder package, which it imports,
// holds no call. The gate needs go1.8, so the fixture is a module of its own, `module differences` at `go 1.8`, under
// go1.8's src, and go1.8's go.work uses both modules, which the harness loads through since it sits beside go1.8's
// go.mod. So the pattern differences names the package at go1.8/src/differences, and the harness reads its records
// from the folder the load finds it in.
var differencesFixtures = []string{"differences", "differences/dotted"}

// modulePatterns is, for each fixture module's version, the patterns that name every package it loads: the module's
// own, and for go1.8 the differences module its workspace uses as well, which ./... stops short of.
var modulePatterns = map[string][]string{
	"go1.7": {"./..."},
	"go1.8": {"./...", "differences/..."},
}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// moduleEnvironment is the environment the fixture module of version loads under, as the parity harness loads a
// fixture directory that holds a go.mod: module mode with nothing fetched, through the go.work beside it where there
// is one, as go1.8's is.
func moduleEnvironment(version string) []string {
	work := filepath.Join(module(version), "go.work")
	if _, statError := os.Stat(work); statError != nil {
		work = "off"
	}
	return []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=" + work}
}

// Every package of every fixture module carries the version its go.mod says, which is what both analyzers' version
// gates read, the differences module's included. The harness loads each module the same way and checks no version, so
// this keeps a load of its own.
func TestFixtureModulesCarryTheirVersions(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		loaded, loadError := load.Packages(module(version), modulePatterns[version], moduleEnvironment(version), nil)
		if loadError != nil {
			test.Fatalf("loading the module of %s: %v", version, loadError)
		}
		if len(loaded.Roots) == 0 {
			test.Fatalf("loading the module of %s found no package, so no version was checked", version)
		}
		for _, root := range loaded.Roots {
			if root.Types.GoVersion() != version {
				test.Errorf("fixture package %s loaded under %q, where its module says %s", root.ID,
					root.Types.GoVersion(), version)
			}
		}
	}
}

// Every `//@ diag(...)` note the go1.8 upstream fixture holds, the port reports on the same line with a message the
// note matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, module("go1.8"), "./"+upstream)
}

// The go1.7 upstream fixture holds no note, so there the original must report nothing, as staticcheck's own test
// requires of it, and the port nothing either; the call it holds is the one the go1.8 fixture's note expects, so the
// gate is what parts them. RequireStaticcheckWants fails fixtures that hold no note and RequireIdentical fails where
// the original finds nothing, since each would prove nothing there, so the silence is held here over the harness's
// Findings.
func TestPortMatchesUpstreamSilenceUnderGo17(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1024.Analyzer, Analyzer, module("go1.7"), "./"+upstream)
	test.Logf("under go1.7 the original found %d, the port %d", len(original), len(port))
	for _, found := range slices.Concat(original, port) {
		test.Errorf("under go1.7 the fixture expects nothing, and %s was found, so the version did not reach it as "+
			"staticcheck's testutil hands it over, or the port's gate parts from the original's", found)
	}
}

// The port's fixes, applied to the go1.8 upstream fixture, make its golden file, as staticcheck's testutil requires
// of the original's. The golden file holds no sections. The go1.7 fixture has no golden file, and no fix.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, module("go1.8"), "./"+upstream)
}

// The original and the port run in one checker over staticcheck's go1.8 fixture and parity's, which holds every shape
// the query branches on and the near miss of each, a generated file, files tagged older and newer than the module,
// and a renamed and a dot import beside time's own name, and their findings must be the same multiset, each the same
// to its end and its fixes. The fix's text is the one the original builds through pattern.NodeToAST and the port
// itself, so this is where the two are held byte for byte, a parenthesized receiver and receivers across lines among
// them. The go1.7 fixture, where the gate leaves both silent, is held by TestPortMatchesUpstreamWants.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1024.Analyzer, Analyzer, module("go1.8"), fixtures...)
}

// Of the original's findings over the fixtures where the two agree, some carry a fix and some do not, so
// TestPortMatchesOriginal holds the port to both branches.
func TestFixturesHoldBothBranches(test *testing.T) {
	test.Parallel()
	original, _ := parity.Findings(test, s1024.Analyzer, Analyzer, module("go1.8"), fixtures...)
	fixes := 0
	for _, finding := range original {
		fixes += len(finding.Fixes)
	}
	test.Logf("of the original's %d findings %d carry a fix", len(original), fixes)
	if fixes == 0 || fixes == len(original) {
		test.Fatalf("of the original's %d findings %d carry a fix, so the fixture does not hold both branches",
			len(original), fixes)
	}
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port moves the fix, writing Until under the name the code gives time and handing it the time.Time itself, or
// withholds it, where no name the code gives calls time.Until or no field path reaches the time.Time.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1024.Analyzer, Analyzer, module("go1.8"), fixture)
		})
	}
}

// Over the differences fixture, each finding the port makes is one the original makes at the same range, and each fix
// it offers edits the bytes the original's does, so a move rewrites a fix's text and nothing else. A `moves:` record
// pairs any two findings on its line with its message, and no harness call holds a moved fix to the original's bytes,
// so this is where it is held there, and TestPortMovedFixesMakeGoldens holds it by its text; a fix the port withholds
// is held to its record by TestPortDiffersFromOriginalOnlyWhereRecorded.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1024.Analyzer, Analyzer, module("go1.8"), differencesFixtures...)
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the comparison proves nothing")
	}
	unpaired := withoutEditText(original)
	for _, portFinding := range withoutEditText(port) {
		index := slices.IndexFunc(unpaired, func(originalFinding parity.Finding) bool {
			if len(portFinding.Fixes) == 0 {
				originalFinding.Fixes = nil
			}
			return len(parity.Difference([]parity.Finding{originalFinding}, []parity.Finding{portFinding})) == 0
		})
		if index < 0 {
			test.Errorf("the port found %s, and no finding of the original's has its range and edits its bytes",
				portFinding)
			continue
		}
		unpaired = slices.Delete(unpaired, index, index+1)
	}
	for _, missing := range unpaired {
		test.Errorf("the original found %s, and the port did not", missing)
	}
}

// The port's fixes, applied to each file of the differences fixture, make the golden file beside it, as gofmt prints
// it, so a move's edits are held by their text: the name the code gives time, the pointer dereferenced and the
// embedded field selected, by its name alone or by the path, which a wrong field that still compiles would fail here
// and nowhere else. Each golden file holds no sections, so it reads as staticcheck's do, and a file a fix edits with
// no golden file fails.
func TestPortMovedFixesMakeGoldens(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, module("go1.8"), differencesFixtures...)
}

// Applied to the differences fixture, the port's fixes make packages that type-check, and the original's make ones
// that do not, in every file: the original names time.Until by a name the file does not give it, or hands it a
// receiver that is no time.Time, and the port names both as the code does, or, where nothing does, writes no fix. The
// fixed files are loaded through an overlay over the fixtures on disk, which changes no import, so go list's answer
// for the workspace holds, and each fixed package must still load under go1.8.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	files := differencesFiles(test)
	original, port := parity.Findings(test, s1024.Analyzer, Analyzer, module("go1.8"), differencesFixtures...)
	portErrors := fixedErrors(test, files, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, files, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	for _, file := range files {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+filepath.Base(file)+":")
		})
		if !isBroken {
			test.Errorf("the original's fixes to %s type-check, so the port's fix there parts from it for nothing",
				filepath.Base(file))
		}
	}
}

// withoutEditText is findings with every edit's new text left out, the part of a moved fix that stays the original's.
func withoutEditText(findings []parity.Finding) []parity.Finding {
	textless := []parity.Finding{}
	for _, finding := range findings {
		fixes := finding.Fixes
		finding.Fixes = nil
		for _, fix := range fixes {
			textlessFix := parity.Fix{Message: fix.Message}
			for _, edit := range fix.Edits {
				edit.NewText = ""
				textlessFix.Edits = append(textlessFix.Edits, edit)
			}
			finding.Fixes = append(finding.Fixes, textlessFix)
		}
		textless = append(textless, finding)
	}
	return textless
}

// differencesFiles is every .go file of the differences fixture's packages, by absolute path. TestPortMovedFixesCompile
// finds a file's type errors by its base name, so two files that share one fail the test.
func differencesFiles(test *testing.T) []string {
	test.Helper()
	files := []string{}
	for _, pattern := range differencesFixtures {
		names, globError := filepath.Glob(
			filepath.Join(module("go1.8"), "src", filepath.FromSlash(pattern), "*.go"),
		)
		if globError != nil {
			test.Fatalf("listing %s: %v", pattern, globError)
		}
		for _, name := range names {
			isShared := slices.ContainsFunc(files, func(other string) bool {
				return filepath.Base(other) == filepath.Base(name)
			})
			if isShared {
				test.Fatalf("%s shares its base name with another file of the fixture, which the findings cannot tell "+
					"apart", name)
			}
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		test.Fatal("the differences fixture holds no file, so the comparison proves nothing")
	}
	return files
}

// fixedErrors is every type error of the differences fixture with the fixes of findings applied to files, loaded
// through an overlay of the fixed files. The harness loads only what is on disk, so this keeps its own load.
func fixedErrors(test *testing.T, files []string, findings []parity.Finding) []string {
	test.Helper()
	directory := module("go1.8")
	overlay := map[string][]byte{}
	for _, file := range files {
		source, readError := os.ReadFile(file)
		if readError != nil {
			test.Fatal(readError)
		}
		// The findings name a file by its path below its module's folder.
		below, relativeError := filepath.Rel(directory, file)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		overlay[file] = applied(test, filepath.Base(file), source, editsIn(findings, filepath.ToSlash(below)))
	}
	// The differences module is loaded through go1.8's workspace, as the harness loads it.
	loaded, loadError := load.PackagesWithOverlay(directory, differencesFixtures, moduleEnvironment("go1.8"), nil,
		overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %v: %v", differencesFixtures, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %v found no package, so the comparison would prove nothing", differencesFixtures)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		if root.Types.GoVersion() != "go1.8" {
			test.Fatalf("fixed package %s loaded under %q, where its module says go1.8", root.ID,
				root.Types.GoVersion())
		}
		for _, each := range root.Errors {
			errors = append(errors, each.Error())
		}
	}
	return errors
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
// were read against. Two edits that overlap fail the test. The harness applies fixes only to hold them to a golden
// file, and here they are held by whether they type-check, so fixedErrors applies them itself.
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
