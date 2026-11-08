package redundant_sprintf_of_string

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1025"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1025, which it keeps four times, under simple/s1025/testdata/go1.0,
// go1.9, go1.17 and go1.18, each with its golden file, each copied unchanged into the module of its folder's name.
// S1025 reads the language version where it judges a slice of a type defined on byte, which converts to a string
// from go1.18 on, so the version matters: staticcheck's testutil loads each folder as a module whose go.mod, overlaid,
// says `go 1.N` for its folder's name, and the go line is what its loader and ours alike hand the type checker as the
// package's version, which code.LanguageVersion reads. So each folder is a module, example.com as staticcheck's is,
// whose go.mod says its folder's version, which the harness loads as a module since the folder holds a go.mod, and
// TestFixtureModulesCarryTheirVersions checks that the version reached every package.
const upstream = "CheckRedundantSprintf"

// upstreamFile is the upstream fixture's one file under every version, beside its golden file.
const upstreamFile = "LintRedundantSprintf.go"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's four.
var versions = []string{"go1.0", "go1.9", "go1.17", "go1.18"}

// moduleEnvironment loads a fixture module as the parity harness loads a fixture directory that holds a go.mod, in
// module mode with nothing fetched.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// agreeingFixtures is every fixture package of each module where the original and the port agree, fixes and all:
// staticcheck's in each, a file tagged past its go1.17 module's version, and in go1.18 parity's with its dot import,
// its package named fmt that is not fmt, and the package that calls it.
var agreeingFixtures = map[string][]string{
	"go1.0":  {"./" + upstream},
	"go1.9":  {"./" + upstream},
	"go1.17": {"./" + upstream, "./tagged"},
	"go1.18": {"./" + upstream, "./parity", "./parity/dotted", "./parity/impostor", "./parity/impostor/fmt"},
}

// differencesFixtures is the differences fixture's packages, in the tree laid out by import path under testdata/src,
// as RequireOnlyAdjudicated reads a differences fixture: its own, one that declares a string of its own at package
// scope, and one that holds the finding the port drops. The tree has no module, so its version is none, which the
// go1.18 judgment of a slice of a type defined on byte reads as older on both sides; the fixture's slices are all of
// byte itself, which converts under every version, and its generics type-check under none as under go1.18.
var differencesFixtures = []string{"differences", "differences/universe", "differences/dropped"}

// sharedDifferencesFixtures is the differences fixture's packages whose findings the two share, whose fixes alone
// part, every package but the one holding the drop.
var sharedDifferencesFixtures = []string{"differences", "differences/universe"}

// regroupedFile is the differences fixture's file whose one move the original's fix compiles at, regrouping the
// argument so it means another value, which no type error shows and the fixed line's text does.
const regroupedFile = "regrouped.go"

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// Every package each fixture module loads carries the version its go.mod says as its own, which both analyzers'
// judgment of a slice of a type defined on byte reads. The harness loads each module as one and reports no package's
// version, and the go1.17 fixture's silence would read the same under no version at all, so this load is the test's
// own, made as the harness makes it.
func TestFixtureModulesCarryTheirVersions(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		loaded, loadError := load.Packages(module(version), agreeingFixtures[version], moduleEnvironment, nil)
		if loadError != nil {
			test.Fatalf("loading %v from %s: %v", agreeingFixtures[version], module(version), loadError)
		}
		if len(loaded.Roots) != len(agreeingFixtures[version]) {
			test.Fatalf("loading %v from %s found %d packages", agreeingFixtures[version], module(version),
				len(loaded.Roots))
		}
		for _, root := range loaded.Roots {
			if root.Types.GoVersion() != version {
				test.Errorf("fixture package %s loaded under %q, where its module says %s", root.ID,
					root.Types.GoVersion(), version)
			}
		}
	}
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.17 fixture holds no note, so there
// the original must report nothing too, as staticcheck's own test requires of it, and the call it holds is the one
// the go1.18 fixture's note expects, so the version is what parts them.
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
		original, port := parity.Findings(test, s1025.Analyzer, Analyzer, module(version), "./"+upstream)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
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

// The port's fixes, applied to the upstream fixture of each version, make its golden file, as staticcheck's testutil
// requires of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when
// it holds no sections, as none of the four does, and the fixed file is gofmt'd before it is compared. The go1.17
// golden file is its fixture unchanged, which no fix touches.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	edited := 0
	for _, version := range versions {
		_, port := parity.Findings(test, s1025.Analyzer, Analyzer, module(version), "./"+upstream)
		path := filepath.Join(module(version), upstream, upstreamFile)
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
			test.Fatalf("the %s golden file holds %d sections, one a fix's message, which this test does not read",
				version, len(archive.Files))
		}
		edits := editsIn(port, upstreamFile)
		edited += len(edits)
		test.Logf("under %s the port found %d, with %d edits", version, len(port), len(edits))
		fixed := applied(test, upstreamFile, source, edits)
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes under %s make source gofmt cannot format: %v\n%s", version, formatError,
				fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes under %s make\n%s\nand the golden file is\n%s", version, formatted,
				archive.Comment)
		}
	}
	if edited == 0 {
		test.Fatal("the port made no edit to the upstream fixtures, so the comparison proves nothing")
	}
}

// Over every module's fixtures where they agree, parity's holding every shape the query and the judgments branch on
// and the near miss of each, a generated file, a renamed and a dot import, a package named fmt that is not fmt, and
// the places each fix stands, the original and the port find the same multiset, each finding the same to its end and
// its fixes. Both build each fix through edit.ReplaceWithNode from the argument the query binds, so this is where the
// binding is held to the original's, its parentheses removed, and every replacement byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		test.Run(version, func(test *testing.T) {
			test.Parallel()
			parity.RequireIdentical(test, s1025.Analyzer, Analyzer, module(version), agreeingFixtures[version]...)
		})
	}
}

// The pattern looks through doubled parentheses around the callee, the format and the argument, and the parser keeps
// a stray semicolon, shapes the house's testdata cannot hold, since its guard holds testdata to gofmt, so the fixture
// is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1025.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port drops one, on a value whose type is an alias of a type parameter, moves a fix, parenthesizing what the
// original's would regroup, or withholds one, where it would leave a bare value as a statement or convert by a string
// that is not the predeclared type.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1025.Analyzer, Analyzer, analysistest.TestData(), fixture)
		})
	}
}

// Over the differences fixture's packages but the drop's, each finding the port makes is one the original makes at
// the same range, and each fix it offers edits the bytes the original's does, so a move rewrites a fix's text and
// nothing else. A `moves:` record pairs any two findings on its line with its message, so this is where the moved fix
// is held to the original's bytes; a fix the port withholds is held to its record by
// TestPortDiffersFromOriginalOnlyWhereRecorded.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1025.Analyzer, Analyzer, analysistest.TestData(),
		sharedDifferencesFixtures...)
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

// Applied to the differences fixture, the port's fixes make packages that type-check, and the original's make ones
// that do not, on the line of every finding whose fix the two write apart, moved or withheld: the original leaves a
// bare value as a statement, regroups an operator under an index, lands a composite literal where the parser reads a
// block, or calls a string that is no type, and the port writes what compiles or nothing. A drop is of a finding whose
// fix compiles, which no fix applied could tell, and so is the move in regroupedFile, whose meaning
// TestPortMovedFixesKeepTheirMeaning holds instead. The fixed files are loaded through an overlay over the fixtures on
// disk, which changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	files := differencesFiles(test)
	original, port := parity.Findings(test, s1025.Analyzer, Analyzer, analysistest.TestData(), differencesFixtures...)
	portErrors := fixedErrors(test, files, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, files, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	parted := 0
	for _, originalFinding := range parity.Difference(original, port) {
		// A finding the port also makes on its line, in its words, is one whose fix it writes apart; one it does not
		// make there is the drop.
		isShared := slices.ContainsFunc(port, func(portFinding parity.Finding) bool {
			return portFinding.File == originalFinding.File && portFinding.Line == originalFinding.Line &&
				portFinding.Message == originalFinding.Message
		})
		if !isShared || originalFinding.File == regroupedFile {
			continue
		}
		parted++
		at := string(filepath.Separator) + originalFinding.File + ":" + strconv.Itoa(originalFinding.Line) + ":"
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, at)
		})
		if !isBroken {
			test.Errorf("the port writes the fix of %s apart from the original's, and the original's fixed line has "+
				"no error", originalFinding)
		}
	}
	test.Logf("the two write %d fixes apart outside %s", parted, regroupedFile)
	if parted == 0 {
		test.Fatal("the two write no fix apart in the differences fixture, so the comparison proves nothing")
	}
}

// The move in regroupedFile is held by the text it writes: applied, the port's fix slices the whole argument,
// `(first + second)[1:]`, and the original's slices second alone, `first + second[1:]`, which compiles, so a port
// that left the parentheses out would pass every other test and change what the code means.
func TestPortMovedFixesKeepTheirMeaning(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1025.Analyzer, Analyzer, analysistest.TestData(), "differences")
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "differences", regroupedFile))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, side := range []struct {
		name     string
		found    []parity.Finding
		expected string
	}{
		{name: "port", found: port, expected: "return (first + second)[1:]"},
		{name: "original", found: original, expected: "return first + second[1:]"},
	} {
		inFile := slices.DeleteFunc(slices.Clone(side.found), func(finding parity.Finding) bool {
			return finding.File != regroupedFile
		})
		if len(inFile) != 1 {
			test.Fatalf("the %s finds %d in %s, where it holds the move alone", side.name, len(inFile), regroupedFile)
		}
		edits := editsIn(inFile, regroupedFile)
		if len(edits) != 1 {
			test.Fatalf("the %s makes %d edits to %s, where the move is one", side.name, len(edits), regroupedFile)
		}
		fixed := strings.Split(string(applied(test, regroupedFile, source, edits)), "\n")
		// Each edit replaces the call within its one line, so the fixed line is the finding's.
		code, _, _ := strings.Cut(fixed[inFile[0].Line-1], " //")
		test.Logf("the %s's fixed line is %q", side.name, strings.TrimSpace(code))
		if strings.TrimSpace(code) != side.expected {
			test.Errorf("the %s's fix makes %q of %s:%d, where %q was expected", side.name, strings.TrimSpace(code),
				regroupedFile, inFile[0].Line, side.expected)
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

// differencesFiles is every .go file of the differences fixture's packages, by absolute path. The findings name a
// file by its base name, so two files that share one fail the test.
func differencesFiles(test *testing.T) []string {
	test.Helper()
	files := []string{}
	for _, pattern := range differencesFixtures {
		names, globError := filepath.Glob(
			filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern), "*.go"),
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

// fixedErrors is every error of the differences fixture with the fixes of findings applied to files, loaded through
// an overlay of the fixed files. The harness loads only what is on disk, so this keeps its own load.
func fixedErrors(test *testing.T, files []string, findings []parity.Finding) []string {
	test.Helper()
	directory := analysistest.TestData()
	overlay := map[string][]byte{}
	for _, file := range files {
		source, readError := os.ReadFile(file)
		if readError != nil {
			test.Fatal(readError)
		}
		overlay[file] = applied(test, filepath.Base(file), source, editsIn(findings, filepath.Base(file)))
	}
	// The fixtures are a tree laid out by import path, loaded as the harness loads one.
	loaded, loadError := load.PackagesWithOverlay(directory, differencesFixtures, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil, overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %v: %v", differencesFixtures, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %v found no package, so the comparison would prove nothing", differencesFixtures)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		for _, each := range root.Errors {
			errors = append(errors, each.Error())
		}
	}
	return errors
}

// editsIn is every edit of every fix of findings in the file named file, by its base name.
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

// unformattedTree writes the unformatted fixture out as Go into a tree laid out by import path in a directory of the
// test's own, and returns the tree. It loads with no module, so its version is none, which the go1.18 judgment of a
// slice of bytes reads as older on both sides.
func unformattedTree(test *testing.T) string {
	test.Helper()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "unformatted", "unformatted.go.txt"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	packageDirectory := filepath.Join(directory, "src", "unformatted")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(packageDirectory, "unformatted.go"), source, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return directory
}
