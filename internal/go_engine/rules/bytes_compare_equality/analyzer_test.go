package bytes_compare_equality

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1004"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1004, copied unchanged from simple/s1004/testdata/go1.0/CheckBytesCompare
// into testdata/src/CheckBytesCompare with its golden file. The go1.0 folder sets the language version of
// staticcheck's run, which S1004 never reads, so the fixture loads from a tree laid out by import path as the house's
// fixtures do.
const upstream = "CheckBytesCompare"

// upstreamFile is the upstream fixture's one file, beside its golden file.
const upstreamFile = "compare.go"

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's, parity's, and the package at bytes' external tests' path, which the check skips.
var fixtures = []string{upstream, "parity", "bytes_test"}

// differencesFixtures is the differences fixture's packages: its own, where a local takes the name bytes over the
// import, one that imports bytes under another name alone, and one that imports bytes into its own scope alone.
var differencesFixtures = []string{"differences", "differences/dotted", "differences/renamed"}

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
	_, port := parity.Findings(test, s1004.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
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

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when it holds no
// sections, and the fixed file is gofmt'd before it is compared, which is what aligns the fixed lines' comments.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := parity.Findings(test, s1004.Analyzer, Analyzer, analysistest.TestData(), upstream)
	path := filepath.Join(analysistest.TestData(), "src", upstream, upstreamFile)
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
		test.Fatalf("the golden file holds %d sections, one a fix's message, which this test does not read",
			len(archive.Files))
	}
	edits := editsIn(port, upstreamFile)
	test.Logf("the port found %d, with %d edits", len(port), len(edits))
	if len(edits) == 0 {
		test.Fatal("the port made no edit to the upstream fixture, so the comparison proves nothing")
	}
	fixed := applied(test, upstreamFile, source, edits)
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
	}
	if !bytes.Equal(formatted, archive.Comment) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", formatted, archive.Comment)
	}
}

// Over staticcheck's fixture, parity's, which holds every shape the query branches on and the near miss of each, a
// generated file and the package at bytes' external tests' path, the original and the port find the same multiset,
// each finding the same to its end and its fixes. The fix's text is the one the original builds through
// edit.ReplaceWithPattern and the port through edit.ReplaceWithNode, so this is where the two are held byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1004.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port moves the fix, writing Equal by the name the file gives it, or withholds it, where no name the file gives
// calls bytes.Equal.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1004.Analyzer, Analyzer, analysistest.TestData(), fixture)
		})
	}
}

// Over the differences fixture, each finding the port makes is one the original makes at the same range, and each fix
// it offers edits the bytes the original's does, so a move rewrites a fix's text and nothing else. A `moves:` record
// pairs any two findings on its line with its message, so this is where the moved fix is held to the original's
// bytes; a fix the port withholds is held to its record by TestPortDiffersFromOriginalOnlyWhereRecorded.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1004.Analyzer, Analyzer, analysistest.TestData(),
		differencesFixtures...)
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
// that do not, in every file: the original names bytes.Equal by a name the file does not give it, and the port by the
// one it does, or, where none does, not at all. The fixed files are loaded through an overlay over the fixtures on
// disk, which changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	files := differencesFiles(test)
	original, port := parity.Findings(test, s1004.Analyzer, Analyzer, analysistest.TestData(),
		differencesFixtures...)
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

// fixedErrors is every type error of the differences fixture with the fixes of findings applied to files, loaded
// through an overlay of the fixed files. The harness loads only what is on disk, so this keeps its own load.
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
