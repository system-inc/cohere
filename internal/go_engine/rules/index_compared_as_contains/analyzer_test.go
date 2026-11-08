package index_compared_as_contains

import (
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/simple/s1003"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1003, copied unchanged from simple/s1003/testdata/go1.0/
// CheckStringsContains into testdata/src/CheckStringsContains with its golden file. The go1.0 folder sets the language
// version of staticcheck's run, which S1003 never reads, so the fixture loads from a tree laid out by import path as
// the house's fixtures do.
const upstream = "CheckStringsContains"

// fixtures is every fixture package where the original and the port agree, fixes and all: staticcheck's, and
// parity's, with its dot import and its generated file.
var fixtures = []string{upstream, "parity"}

// differencesFixture is the differences fixture's package, where the port parts from the original only where a record
// says so.
const differencesFixture = "differences"

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, so its comment is the whole fixed file, and gofmt, which the fixed
// file passes through before it is compared, is what aligns the fixed lines' comments.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// pairing of value and operator the table allows or refuses, the right operand's constant spelled every way, the
// left operand parenthesized and swapped, each function the check knows beside its neighbors, a generated file and a
// dot import, and their findings must be the same multiset, each the same to its end and its fixes. Both build the
// fix's syntax and print it through edit.ReplaceWithNode, so this is where the two are held byte for byte, negations
// included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1003.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// In the differences fixture, the port's findings are the original's but where a record says the port drops one, a
// qualifier that is no import of strings or bytes, or finds one, a renamed import of either. Every finding no record
// is on is the same on both sides to its end and its fixes, so where the two agree there, an import of bytes under
// the name strings and arguments across lines among them, they agree on fixes too.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1003.Analyzer, Analyzer, analysistest.TestData(), differencesFixture)
}

// Applied to the differences fixture, the port's fixes make a package that gofmt formats and that type-checks, the
// renamed imports' fixes, which the original cannot vouch for, among them, each finding the port alone makes with a
// fix of its own, and the original's make one that does not type-check in every file it alone finds something in: it
// calls a Contains that a variable's type, or a package of another path named strings, does not declare. The fixed
// files are loaded through an overlay over the fixture on disk, which changes no import, so go list's answer for the
// tree holds.
func TestPortFixesCompile(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1003.Analyzer, Analyzer, analysistest.TestData(), differencesFixture)
	portAlone := parity.Difference(port, original)
	originalAlone := parity.Difference(original, port)
	test.Logf("the port alone found %d, the original alone %d", len(portAlone), len(originalAlone))
	if len(portAlone) == 0 || len(originalAlone) == 0 {
		test.Fatal("the two part nowhere in the differences fixture, so the comparison proves nothing")
	}
	for _, each := range portAlone {
		if len(editsIn([]parity.Finding{each}, each.File)) == 0 {
			test.Errorf("the port alone found %s, and fixes nothing there", each)
		}
	}
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	for _, each := range originalAlone {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+each.File+":")
		})
		if !isBroken {
			test.Errorf("the original alone found %s, and its fixes to %s type-check", each, each.File)
		}
	}
}

// fixedErrors is every type error of the differences fixture with the fixes of findings applied, each file's edits
// from the last to the first and the result gofmt'd, loaded through an overlay of the fixed files. A fixed file gofmt
// cannot format fails the test. The harness loads only what is on disk, so this keeps its own load.
func fixedErrors(test *testing.T, findings []parity.Finding) []string {
	test.Helper()
	directory := analysistest.TestData()
	names, globError := filepath.Glob(filepath.Join(directory, "src", differencesFixture, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", differencesFixture, globError)
	}
	overlay := map[string][]byte{}
	for _, name := range names {
		source, readError := os.ReadFile(name)
		if readError != nil {
			test.Fatal(readError)
		}
		// The findings name a file by its path below testdata's src.
		below, relativeError := filepath.Rel(filepath.Join(directory, "src"), name)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		fixed := applied(test, filepath.Base(name), source, editsIn(findings, filepath.ToSlash(below)))
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the fixes to %s make source gofmt cannot format: %v\n%s", filepath.Base(name), formatError,
				fixed)
		}
		overlay[name] = formatted
	}
	// The fixtures are a tree laid out by import path, loaded as the harness loads one.
	loaded, loadError := load.PackagesWithOverlay(directory, []string{differencesFixture}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil, overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %s: %v", differencesFixture, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %s found no package, so the comparison would prove nothing", differencesFixture)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		for _, each := range root.Errors {
			errors = append(errors, each.Error())
		}
	}
	return errors
}

// editsIn is every edit of every fix of findings in the file named file, by its path below testdata's src.
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
// file, and the differences fixture's are held instead by whether they type-check, so fixedErrors applies them here.
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
