package boolean_constant_comparison

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/simple/s1002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstreams are staticcheck's own fixtures for S1002, each copied unchanged, golden file and all, from
// simple/s1002/testdata/go1.0/CheckIfBoolCmp and go1.18/CheckIfBoolCmp into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1002 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language. The first holds a test file, which the
// check leaves alone.
var upstreams = []string{"go1.0/CheckIfBoolCmp", "go1.18/CheckIfBoolCmp"}

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's two and parity's, which imports a package of constants.
var fixtures = []string{"go1.0/CheckIfBoolCmp", "go1.18/CheckIfBoolCmp", "parity"}

// differences is the differences fixture's one package, where the two make the same findings and part on some
// fixes.
const differences = "differences"

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects. The go1.0 package has in-package tests, so it is held alone
// and with them, and its notes in each.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The port's fixes, applied to each upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. Each finding suggests one fix, and neither golden file holds sections. The go1.0 package's golden file
// is held to its fixes alone and again with its in-package tests.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstreams...)
}

// The original and the port run in one checker over staticcheck's fixtures and parity's, which holds every shape the
// judgment branches on and the near miss of each, a generated file, a test file, line directives, a dot import and a
// package of constants, and their findings must be the same multiset, each the same to its end and its fixes. Both
// write the fix through edit.ReplaceWithString from the other side's render, so this is where the render and the
// folding of negations are held byte for byte, across lines and with comments among them.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1002.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// Doubled parentheses around either side and comparisons written without spaces are shapes the house's testdata
// cannot hold, since its guard holds testdata to gofmt, so the fixture is kept as
// testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own. The two are held the
// same there to each finding's end and its fixes.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1002.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// In the differences fixture, the port's findings are the original's but where a `moves:` record says the port
// negates a comparison whole where the original negates its leftmost operand, which no negation takes.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1002.Analyzer, Analyzer, analysistest.TestData(), differences)
}

// Over the differences fixture, each finding the port makes is one the original makes at the same range, and each fix
// it offers edits the bytes the original's does, so a move rewrites a fix's text and nothing else, and where it does,
// the port's text is the original's with its negation taken over the comparison whole, `!a < b` written `!(a < b)`.
// A `moves:` record pairs any two findings on its line with its message, so this is where the moved fix is held to
// the original's bytes and to the one text the move writes.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1002.Analyzer, Analyzer, analysistest.TestData(), differences)
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the comparison proves nothing")
	}
	unpaired := slices.Clone(original)
	for _, portFinding := range port {
		index := slices.IndexFunc(unpaired, func(originalFinding parity.Finding) bool {
			return len(parity.Difference(withoutEditText([]parity.Finding{originalFinding}),
				withoutEditText([]parity.Finding{portFinding}))) == 0
		})
		if index < 0 {
			test.Errorf("the port found %s, and no finding of the original's has its range and edits its bytes",
				portFinding)
			continue
		}
		originalTexts, portTexts := editTexts(unpaired[index]), editTexts(portFinding)
		for textIndex, portText := range portTexts {
			originalText := originalTexts[textIndex]
			if portText != originalText && portText != "!("+strings.TrimPrefix(originalText, "!")+")" {
				test.Errorf("the port found %s, whose fix writes %q where the original writes %q, which is not the "+
					"original's negation taken whole", portFinding, portText, originalText)
			}
		}
		unpaired = slices.Delete(unpaired, index, index+1)
	}
	for _, missing := range unpaired {
		test.Errorf("the original found %s, and the port did not", missing)
	}
}

// Applied to the differences fixture, the port's fixes make a package that type-checks, and the original's make one
// that does not, with an error on every line where the port's fix parts from the original's: the original negates an
// operand no negation takes, and the port negates the comparison whole. The fixed files are loaded through an overlay
// over the fixtures on disk, which changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1002.Analyzer, Analyzer, analysistest.TestData(), differences)
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	moved := parity.Difference(original, port)
	test.Logf("the port's fixed fixture has %d errors, the original's %d, over %d moved fixes", len(portErrors),
		len(originalErrors), len(moved))
	if len(moved) == 0 {
		test.Fatal("the port's fixes are the original's everywhere, so the comparison proves nothing")
	}
	for _, each := range moved {
		// A fix replaces the comparison on its own line with text of one line, so the fixed file's lines are the
		// fixture's, and an error the original's fix makes is on the line of its finding.
		place := fmt.Sprintf("%c%s:%d:", filepath.Separator, each.File, each.Line)
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, place)
		})
		if !isBroken {
			test.Errorf("the port's fix at %s:%d parts from the original's, and the original's there type-checks",
				each.File, each.Line)
		}
	}
}

// fixedErrors is every type error of the differences fixture with the fixes of findings applied, loaded through an
// overlay of the fixed files. The harness loads only what is on disk, so this keeps its own load.
func fixedErrors(test *testing.T, findings []parity.Finding) []string {
	test.Helper()
	directory := analysistest.TestData()
	names, globError := filepath.Glob(filepath.Join(directory, "src", differences, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", differences, globError)
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
		overlay[name] = applied(test, filepath.Base(name), source, editsIn(findings, filepath.ToSlash(below)))
	}
	// The fixtures are a tree laid out by import path, loaded as the harness loads one.
	loaded, loadError := load.PackagesWithOverlay(directory, []string{differences}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil, overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %s: %v", differences, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %s found no package, so the comparison would prove nothing", differences)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		for _, each := range root.Errors {
			errors = append(errors, each.Error())
		}
	}
	return errors
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

// editTexts is the new text of every edit of every fix of finding, in order.
func editTexts(finding parity.Finding) []string {
	texts := []string{}
	for _, fix := range finding.Fixes {
		for _, edit := range fix.Edits {
			texts = append(texts, edit.NewText)
		}
	}
	return texts
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
// file, and the moved fixes are held instead by whether they type-check, so fixedErrors applies them here.
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
// test's own, and returns the tree. No call of the parity harness writes a fixture out before it loads it, so this
// stays the port's own.
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
