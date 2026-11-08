package errors_new_of_sprintf

import (
	"cmp"
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/simple/s1028"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1028, copied unchanged from
// simple/s1028/testdata/go1.0/CheckErrorsNewSprintf into testdata/src/CheckErrorsNewSprintf with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which S1028 never reads, gating nothing, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckErrorsNewSprintf"

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's, parity's, parity's dot and renamed imports beside fmt's own name, and the package at fmt's external
// tests' path, which imports fmt as any other package does.
var fixtures = []string{upstream, "parity", "parity/dotted", "fmt_test"}

// differencesFixtures is the differences fixture's packages: its own, which imports fmt under another name alone, one
// that imports fmt into its own scope alone, one where a local takes the name fmt, one that names another package
// fmt, and one that imports fmt under its own name and spreads a list into Sprintf.
var differencesFixtures = []string{
	"differences", "differences/dotted", "differences/shadowed", "differences/spelled", "differences/spread",
}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, so it is the whole fixed file.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture, parity's, which holds every shape the
// query branches on and the near miss of each, a generated file, and dot and renamed imports, and the package at fmt's
// external tests' path, and their findings must be the same multiset, each the same to its end and its fixes. The
// original builds its fix through code.EditMatch from the arguments its query binds, and the port through
// edit.ReplaceWithNode from the ones it binds, so this is where the two are held byte for byte, dot and renamed
// imports beside fmt's own name, arguments across lines and comments among them included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1028.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port moves the fix, writing Errorf by the name the code gives fmt and keeping a spread's ellipsis, or withholds it,
// where no name the code gives calls fmt.Errorf.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1028.Analyzer, Analyzer, analysistest.TestData(), fixture)
		})
	}
}

// Over the differences fixture, each finding the port makes is one the original makes at the same range, and each fix
// it offers edits the bytes the original's does, so a move rewrites a fix's text and nothing else. A `moves:` record
// pairs any two findings on its line with its message, so this is where the moved fix is held to the original's
// bytes, and TestPortMovedFixesMakeGoldens holds it by its text; a fix the port withholds is held to its record by
// TestPortDiffersFromOriginalOnlyWhereRecorded.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1028.Analyzer, Analyzer, analysistest.TestData(),
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

// The port's fixes, applied to each differences file, make the file's golden file beside it, as gofmt prints it, so
// a move's edits are held by their text: the name the code calls Sprintf by in place of fmt, Errorf alone under a dot
// import, and a spread's ellipsis kept. Every file of the differences fixture is one a move is in, and each has a
// golden file in the form staticcheck's fixtures give theirs, with no sections, so the harness holds them as it holds
// those, and a fix in a file with none fails.
func TestPortMovedFixesMakeGoldens(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), differencesFixtures...)
}

// Applied to the differences fixture, the port's fixes make packages that type-check, and every finding where the
// two part is one the original's fix gets wrong: either its fixes make the finding's file fail to type-check, where
// the original names fmt.Errorf by a name the code does not give it and the port by the one it does, or, where none
// does, not at all; or the original's fix drops the ellipsis of a spread into Sprintf, which still type-checks and
// hands Errorf the list as one value, and the port's keeps it. The fixed files are loaded through an overlay over the
// fixtures on disk, which changes no import, so go list's answer for the tree holds. No harness call applies fixes and
// type-checks the result or reads the call a fix writes, so this test keeps its own helpers for it.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	files := differencesFiles(test)
	original, port := parity.Findings(test, s1028.Analyzer, Analyzer, analysistest.TestData(),
		differencesFixtures...)
	portErrors := fixedErrors(test, files, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, files, original)
	parted := parity.Difference(original, port)
	if len(parted) == 0 {
		test.Fatal("the port parts from the original nowhere in the differences fixture, so the comparison proves " +
			"nothing")
	}
	spreadsKept := 0
	for _, finding := range parted {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+finding.File+":")
		})
		isSpreadKept := keepsDroppedSpread(test, original, port, finding)
		if isSpreadKept {
			spreadsKept++
		}
		if !isBroken && !isSpreadKept {
			test.Errorf("the port parts from the original's %s, and the original's fixes to %s type-check and drop "+
				"no spread the port's keep", finding, finding.File)
		}
	}
	test.Logf("the two part on %d findings, the port's fixed fixture has %d errors, the original's %d, and the port "+
		"keeps %d spreads the original drops", len(parted), len(portErrors), len(originalErrors), spreadsKept)
}

// keepsDroppedSpread is whether, at the line of the original's finding at, the original's one fix writes a call with
// no ellipsis and the port's writes it with one.
func keepsDroppedSpread(test *testing.T, original []parity.Finding, port []parity.Finding, at parity.Finding) bool {
	test.Helper()
	originalCall := fixedCall(test, original, at)
	portCall := fixedCall(test, port, at)
	return originalCall != nil && portCall != nil && !originalCall.Ellipsis.IsValid() && portCall.Ellipsis.IsValid()
}

// fixedCall is the call the one edit of the one fix of the finding on the line of at writes, parsed, or nil where no
// finding there has one.
func fixedCall(test *testing.T, findings []parity.Finding, at parity.Finding) *ast.CallExpr {
	test.Helper()
	for _, candidate := range findings {
		if candidate.File != at.File || candidate.Line != at.Line || len(candidate.Fixes) != 1 ||
			len(candidate.Fixes[0].Edits) != 1 {
			continue
		}
		expression, parseError := parser.ParseExpr(candidate.Fixes[0].Edits[0].NewText)
		if parseError != nil {
			test.Fatalf("%s:%d: the fix writes %q, which is no expression: %v", at.File, at.Line,
				candidate.Fixes[0].Edits[0].NewText, parseError)
		}
		call, isCall := expression.(*ast.CallExpr)
		if !isCall {
			test.Fatalf("%s:%d: the fix writes %q, which is no call", at.File, at.Line,
				candidate.Fixes[0].Edits[0].NewText)
		}
		return call
	}
	return nil
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

// differencesFiles is every .go file of the differences fixture's packages, by absolute path. The findings and the
// type errors TestPortMovedFixesCompile reads name a file by its path below testdata's src, so no two are confused.
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
		files = append(files, names...)
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
		// The findings name a file by its path below testdata's src.
		below, relativeError := filepath.Rel(filepath.Join(directory, "src"), file)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		overlay[file] = applied(test, filepath.Base(file), source, editsIn(findings, filepath.ToSlash(below)))
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
// file, and fixedErrors type-checks what they make, so this stays the port's own.
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
