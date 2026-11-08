package type_switch_bound_value

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"honnef.co/go/tools/simple/s1034"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstream is staticcheck's own fixture for S1034, copied unchanged from
// simple/s1034/testdata/go1.0/CheckSimplifyTypeSwitch into testdata/src/CheckSimplifyTypeSwitch with its golden file.
// The go1.0 folder sets the language version of staticcheck's run, which S1034 never reads, so the fixture loads from
// a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSimplifyTypeSwitch"

// differences is the differences fixture's one package, a file for each shape where the port parts from the original
// and one of shapes where the two agree, which holds no package of its own beneath it.
const differences = "differences"

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's and parity's.
var fixtures = []string{upstream, "parity"}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, so its comment is the whole fixed file.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture and parity's, the second holding every shape the query and the judgment of each case
// branch on, the near miss of each, and a generated file, the original and the port find the same multiset, each
// finding the same to its end, its related entries and its fixes. The binding's text is the one the original builds
// through edit.ReplaceWithPattern and the port through edit.ReplaceWithNode, so this is where the two are held byte
// for byte, parentheses and comments in the guard included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1034.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The shapes gofmt rewrites, a guard and an assertion split across lines and doubled parentheses around the switched
// variable, are held the same way, their ends, related entries and fixes too, since the printer reads the positions
// of what it prints. The guard holds every .go file of the tree to gofmt, so the fixture is kept as
// testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own, which the harness
// loads as it loads testdata.
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
	parity.RequireIdentical(test, s1034.Analyzer, Analyzer, directory, "unformatted")
}

// In the differences fixture, the port's findings are the original's but where a record says the port drops one, a
// switch whose only assertion is in a case's type, which binding the value can't eliminate; moves the fix, writing
// `x, true` over a comma-ok assertion, or leaving an assertion in a case's type as it is, with the related entry the
// original gives it; or withholds the fix, where the switch writes to x or a clause declares its own x. A move pairs
// the two findings by line and message alone, so the moved fix's text is held by TestPortMovedFixesMakeGoldens, and
// its range and related entries by TestPortMovedFindingsRelateWhatTheirFixReplaces.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1034.Analyzer, Analyzer, analysistest.TestData(), differences)
}

// Over the differences fixture, a moved finding is the original's at the same range, and its related entries are the
// original's, less each one on an assertion the port's fix leaves in place: every related entry the port gives, the
// original gives too, on an assertion an edit of the port's fix replaces, and every one the original gives that the
// port leaves out is on an assertion no edit of the port's touches, as `x.([8]byte)` in a case's type, which the
// binding can't eliminate. A `moves:` record pairs two findings by line and message alone, so this is where a moved
// finding's range and related entries are held, as TestPortMovedFixesMakeGoldens holds its fix's text. No call of the
// parity harness holds a moved finding's range or related entries, so this stays the port's own.
func TestPortMovedFindingsRelateWhatTheirFixReplaces(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1034.Analyzer, Analyzer, analysistest.TestData(), differences)
	moves := movesOf(original, port)
	if len(moves) == 0 {
		test.Fatal("the port moved no fix in the differences fixture, so the comparison proves nothing")
	}
	omitted := 0
	for _, moved := range moves {
		if moved.port.Column != moved.original.Column || moved.port.EndLine != moved.original.EndLine ||
			moved.port.EndColumn != moved.original.EndColumn {
			test.Errorf("the port found %s, at another range than the original's %s", moved.port, moved.original)
		}
		// The findings name a file by its path below testdata's src.
		source, readError := os.ReadFile(
			filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(moved.port.File)),
		)
		if readError != nil {
			test.Fatal(readError)
		}
		for _, related := range moved.port.Related {
			if !slices.Contains(moved.original.Related, related) {
				test.Errorf("the port's %s relates %s:%d:%d, and the original's does not", moved.port, related.File,
					related.Line, related.Column)
			}
		}
		for _, related := range moved.original.Related {
			isKept := slices.Contains(moved.port.Related, related)
			isReplaced := isEditedBy(source, related, moved.port.Fixes)
			switch {
			case isKept && !isReplaced:
				test.Errorf("the port's %s says the assertion at %s:%d:%d could be eliminated, and its fix leaves it "+
					"in place", moved.port, related.File, related.Line, related.Column)
			case !isKept && isReplaced:
				test.Errorf("the port's %s leaves out the related entry at %s:%d:%d, on an assertion its fix replaces",
					moved.port, related.File, related.Line, related.Column)
			case !isKept:
				omitted++
			}
		}
	}
	test.Logf("the port moved %d of the original's findings, leaving out %d of their related entries", len(moves),
		omitted)
}

// The port's fixes, applied to each differences file a finding moves in, make the file's golden file, as gofmt prints
// it, so a move's edits are held by their text: `x, true` written over a comma-ok assertion's parentheses, and the
// assertion in a case's type left as it is, one edit fewer than the original's. Every golden file belongs to a file a
// move is in. The parity harness's RequireStaticcheckGolden holds every file a fix edits to a golden file, and the
// port fixes differences.go too, where the two agree and no golden file is kept, so this stays the port's own.
func TestPortMovedFixesMakeGoldens(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1034.Analyzer, Analyzer, analysistest.TestData(), differences)
	moved := []string{}
	for _, each := range movesOf(original, port) {
		// The findings name a file by its path below testdata's src, and moved names it within the fixture's folder.
		file := strings.TrimPrefix(each.port.File, differences+"/")
		if !slices.Contains(moved, file) {
			moved = append(moved, file)
		}
	}
	if len(moved) == 0 {
		test.Fatal("the port moved no fix in the differences fixture, so the comparison proves nothing")
	}
	directory := filepath.Join(analysistest.TestData(), "src", differences)
	goldens, globError := filepath.Glob(filepath.Join(directory, "*.golden"))
	if globError != nil {
		test.Fatalf("listing %s: %v", directory, globError)
	}
	for _, golden := range goldens {
		if !slices.Contains(moved, strings.TrimSuffix(filepath.Base(golden), ".golden")) {
			test.Errorf("%s is the golden file of a file no move is in", filepath.Base(golden))
		}
	}
	for _, file := range moved {
		path := filepath.Join(directory, file)
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		golden, goldenError := os.ReadFile(path + ".golden")
		if goldenError != nil {
			test.Fatal(goldenError)
		}
		edits := editsIn(port, differences+"/"+file)
		test.Logf("%s: the port made %d edits", file, len(edits))
		formatted, formatError := format.Source(applied(test, file, source, edits))
		if formatError != nil {
			test.Fatalf("the port's fixes to %s make source gofmt cannot format: %v", file, formatError)
		}
		if !bytes.Equal(formatted, golden) {
			test.Errorf("the port's fixes to %s make\n%s\nand its golden file is\n%s", file, formatted, golden)
		}
	}
}

// Applied to the differences fixture, the port's fixes make a package that type-checks, and the original's make one
// that does not in every file a finding is dropped or moved in: the original assigns one value to two where an
// assertion was comma-ok, and gives a case's type the size of an interface, binding a value no case uses or giving
// eight bytes sixteen. A withheld fix needs no break: the original's fix where the default clause takes x's address
// compiles, and writes to a copy. The fixed files are loaded through an overlay over the fixtures on disk, which
// changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1034.Analyzer, Analyzer, analysistest.TestData(), differences)
	parted := []string{}
	for _, each := range movesOf(original, port) {
		if !slices.Contains(parted, each.original.File) {
			parted = append(parted, each.original.File)
		}
	}
	for _, each := range droppedOf(original, port) {
		if !slices.Contains(parted, each.File) {
			parted = append(parted, each.File)
		}
	}
	if len(parted) == 0 {
		test.Fatal("the port moves and drops nothing in the differences fixture, so the comparison proves nothing")
	}
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d, over %d files a finding moves or drops in",
		len(portErrors), len(originalErrors), len(parted))
	for _, file := range parted {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+file+":")
		})
		if !isBroken {
			test.Errorf("the port moves or drops the original's finding in %s, and the original's fixes to it "+
				"type-check", file)
		}
	}
}

// move is one finding both sides make on the same line with the same message, each offering a fix, the two differing:
// the original's and the port's.
type move struct {
	original parity.Finding
	port     parity.Finding
}

// movesOf is every finding of port's that a finding of original's on its line with its message pairs as a move, both
// offering a fix, and the two differing in any field. A finding whose fix the port withholds offers none, and its
// record holds it whole beside the original's.
func movesOf(original []parity.Finding, port []parity.Finding) []move {
	moves := []move{}
	for _, portFinding := range port {
		index := slices.IndexFunc(original, func(originalFinding parity.Finding) bool {
			return isAtTheSamePlace(originalFinding, portFinding)
		})
		if index < 0 || len(original[index].Fixes) == 0 || len(portFinding.Fixes) == 0 {
			continue
		}
		if len(parity.Difference([]parity.Finding{original[index]}, []parity.Finding{portFinding})) > 0 {
			moves = append(moves, move{original: original[index], port: portFinding})
		}
	}
	return moves
}

// droppedOf is every finding of original's that no finding of port's makes on its line with its message.
func droppedOf(original []parity.Finding, port []parity.Finding) []parity.Finding {
	dropped := []parity.Finding{}
	for _, originalFinding := range original {
		if !slices.ContainsFunc(port, func(portFinding parity.Finding) bool {
			return isAtTheSamePlace(originalFinding, portFinding)
		}) {
			dropped = append(dropped, originalFinding)
		}
	}
	return dropped
}

// isAtTheSamePlace is whether first and second are in the same file on the same line with the same message, which is
// all a record names of a finding.
func isAtTheSamePlace(first parity.Finding, second parity.Finding) bool {
	return first.File == second.File && first.Line == second.Line && first.Message == second.Message
}

// isEditedBy is whether an edit of fixes overlaps the range related names in source, the file it is in.
func isEditedBy(source []byte, related parity.Related, fixes []parity.Fix) bool {
	start := offsetOf(source, related.Line, related.Column)
	end := offsetOf(source, related.EndLine, related.EndColumn)
	for _, fix := range fixes {
		for _, edit := range fix.Edits {
			if edit.File == related.File && edit.Start < end && start < edit.End {
				return true
			}
		}
	}
	return false
}

// offsetOf is the byte offset in source of a line and a column, each counted from 1, the column in bytes, as a
// token.Position counts them.
func offsetOf(source []byte, line int, column int) int {
	offset := 0
	for range line - 1 {
		offset += bytes.IndexByte(source[offset:], '\n') + 1
	}
	return offset + column - 1
}

// fixedErrors is every type error of the differences fixture with the fixes of findings applied, each file's edits
// from the last to the first, loaded through an overlay of the fixed files. The harness loads only what is on disk,
// so this keeps its own load.
func fixedErrors(test *testing.T, findings []parity.Finding) []string {
	test.Helper()
	directory := analysistest.TestData()
	names, globError := filepath.Glob(filepath.Join(directory, "src", differences, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", differences, globError)
	}
	if len(names) == 0 {
		test.Fatal("the differences fixture holds no file, so the comparison proves nothing")
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

// editsIn is every edit of every fix of findings in the file named file, by its path below testdata's src, for the
// tests above that apply the port's fixes where no call of the parity harness does.
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
// were read against. Two edits that overlap fail the test. The parity harness applies fixes only inside
// RequireStaticcheckGolden, so the moved goldens and the type-check of the fixed fixture keep their own.
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
