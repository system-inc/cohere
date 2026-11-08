package guarded_map_access

import (
	"bytes"
	"cmp"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1036"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1036, copied unchanged from
// simple/s1036/testdata/go1.0/CheckUnnecessaryGuard into testdata/src/CheckUnnecessaryGuard with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which S1036 never reads, so the fixture loads from a
// tree laid out by import path as the house's fixtures do.
const upstream = "CheckUnnecessaryGuard"

// upstreamFile is the upstream fixture's one file, beside its golden file.
const upstreamFile = "LintUnnecessaryGuard.go"

// differences is the differences fixture's one package, which holds no package of its own beneath it.
const differences = "differences"

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's; parity, which holds every shape the query branches on and the near miss of each, with a generated
// file, which neither filters; and shadow, which declares an append of its own at package scope.
var fixtures = []string{upstream, "parity", "shadow"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any other name as readily; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1036.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1036.Analyzer.Name)
	}
}

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
	_, port := parity.Findings(test, s1036.Analyzer, Analyzer, analysistest.TestData(), upstream)
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
// sections, and the fixed file is gofmt'd before it is compared, which is what closes the blank lines around each if
// the fix replaced.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := parity.Findings(test, s1036.Analyzer, Analyzer, analysistest.TestData(), upstream)
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

// Over staticcheck's fixture and the parity fixtures, the original and the port find the same multiset, each finding
// the same to its end and its fixes. The fix's text is format.Node of the if's one body statement on both sides, so
// this is where the two are held byte for byte, a split call with comments among its arguments included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1036.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The shapes gofmt rewrites, a condition in parentheses, which gofmt strips and the pattern looks through, and
// doubled parentheses elsewhere, are held the same way. The guard holds every .go file of the tree to gofmt, so the
// fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own,
// which the harness loads as it loads testdata.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
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
	parity.RequireIdentical(test, s1036.Analyzer, Analyzer, directory, "unformatted")
}

// In the differences fixture, the port's findings are the original's but where a record says the port drops one, a
// guard the zero value does not cover: an append that spreads its last argument against a literal that holds that
// argument as one element, and an append of nothing against an empty literal, which is nil where the literal is not.
// Or where a record says the port moves the fix, of a guard that is another if's else branch, where the original's
// bare statement, `} else m[k]++`, does not parse, and the port writes the if's body, a block.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1036.Analyzer, Analyzer, analysistest.TestData(), differences)
}

// Over the differences fixture, each finding the port makes is one the original makes at the same range, and each fix
// it offers edits the bytes the original's does, so a move rewrites a fix's text and nothing else. Where the text
// differs, the original's edit, applied alone, makes a file that does not parse. A `moves:` record pairs any two
// findings on its line with its message, so this is where the moved fix is held to the original's bytes, and where
// each move is held to a fix the original breaks; what the original alone finds is the drops, which
// TestPortDiffersFromOriginalOnlyWhereRecorded holds to their records.
func TestPortMovesOnlyTheTextOfAFix(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1036.Analyzer, Analyzer, analysistest.TestData(), differences)
	unpaired := slices.Clone(original)
	moved := 0
	for _, portFinding := range port {
		index := slices.IndexFunc(unpaired, func(originalFinding parity.Finding) bool {
			return len(parity.Difference(
				[]parity.Finding{withoutEditText(originalFinding)}, []parity.Finding{withoutEditText(portFinding)},
			)) == 0
		})
		if index < 0 {
			test.Errorf("the port found %s, and no finding of the original's has its range and edits its bytes",
				portFinding)
			continue
		}
		originalFinding := unpaired[index]
		unpaired = slices.Delete(unpaired, index, index+1)
		if len(parity.Difference([]parity.Finding{originalFinding}, []parity.Finding{portFinding})) == 0 {
			continue
		}
		moved++
		source, readError := os.ReadFile(
			filepath.Join(analysistest.TestData(), "src", differences, originalFinding.File),
		)
		if readError != nil {
			test.Fatal(readError)
		}
		fixed := applied(test, originalFinding.File, source,
			editsIn([]parity.Finding{originalFinding}, originalFinding.File))
		if _, parseError := parser.ParseFile(token.NewFileSet(), originalFinding.File, fixed,
			parser.SkipObjectResolution); parseError == nil {
			test.Errorf("the original's fix of %s parses, so the port's fix there moves it for nothing",
				originalFinding)
		}
	}
	test.Logf("the original found %d, the port %d, of which %d moved and the original alone %d", len(original),
		len(port), moved, len(unpaired))
	if moved == 0 {
		test.Fatal("the port moved no fix in the differences fixture, so the comparison proves nothing")
	}
}

// Applied to the differences fixture, the port's fixes make a package that type-checks, and the original's make one
// that does not, as each recorded move says, so a move that fixed nothing broken, or a port fix that breaks what it
// touches, fails here. The fixed files are loaded through an overlay over the fixtures on disk, which changes no
// import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	files := differencesFiles(test)
	original, port := parity.Findings(test, s1036.Analyzer, Analyzer, analysistest.TestData(), differences)
	if len(editsIn(port, filepath.Base(files[0]))) == 0 {
		test.Fatal("the port made no edit to the differences fixture, so the comparison proves nothing")
	}
	portErrors := fixedErrors(test, files, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, files, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	if len(originalErrors) == 0 {
		test.Error("the original's fixes to the differences fixture type-check, so no move fixes anything")
	}
}

// withoutEditText is finding with every edit's new text left out, the part of a moved fix that stays the original's.
func withoutEditText(finding parity.Finding) parity.Finding {
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
	return finding
}

// differencesFiles is every .go file of the differences fixture, by absolute path.
func differencesFiles(test *testing.T) []string {
	test.Helper()
	files, globError := filepath.Glob(filepath.Join(analysistest.TestData(), "src", differences, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", differences, globError)
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
