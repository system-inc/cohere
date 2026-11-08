package separate_declaration_and_assignment

import (
	"cmp"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1021"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// upstream is staticcheck's own fixture for S1021, copied unchanged from simple/s1021/testdata/go1.0/CheckDeclareAssign
// into testdata/src/CheckDeclareAssign with its golden file. The go1.0 folder sets the language version of
// staticcheck's run, which S1021 never reads, so the fixture loads from a tree laid out by import path as the house's
// fixtures do.
const upstream = "CheckDeclareAssign"

// identical is every fixture package where the two agree finding for finding: staticcheck's and parity's, which holds
// a generated file. The differences fixture is apart, since there the port finds what the original misses.
var identical = []string{upstream, "parity"}

// Every `//@ diag(...)` note the upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// The port's fixes, applied to the upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's. The golden file holds no sections, and gofmt is what aligns the fixed lines' comments.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckGolden(test, Analyzer, analysistest.TestData(), upstream)
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the check branches on and the near miss of each, and a generated file, and their findings must be the same
// multiset, each the same to its end and its fixes. Both build the fix's declaration from the same nodes and print it
// through edit.ReplaceWithNode, so this is where the text is held byte for byte, a value across lines, a composite
// literal and comments before, inside and after the value among them.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1021.Analyzer, Analyzer, analysistest.TestData(), identical...)
}

// The differences fixture records each declaration in a switch's or a select's clause, which the port alone finds,
// with the text of its fix, and holds the clause shapes both leave alone and the nested block both find, and nothing
// the original finds is missing from the port's.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1021.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// In the differences fixture, the port's fixes, its own findings' among them, applied together, make a file gofmt
// formats and the type checker accepts, so a clause's merged declaration is as sound as a block's. A `fixes:` record
// holds where an edit starts and what it writes, not where it ends, so this holds the rest.
func TestPortFixesInClausesCompile(test *testing.T) {
	test.Parallel()
	_, port := parity.Findings(test, s1021.Analyzer, Analyzer, analysistest.TestData(), "differences")
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	formatted, edits := fixed(test, path, port)
	test.Logf("the port's %d edits applied", edits)
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "differences.go", formatted, 0)
	if parseError != nil {
		test.Fatalf("the port's fixes make source that does not parse: %v\n%s", parseError, formatted)
	}
	// The fixture imports nothing, so the type checker needs no importer.
	if _, checkError := (&types.Config{}).Check("differences", fileSet, []*ast.File{file}, nil); checkError != nil {
		test.Errorf("the port's fixes make source that does not type-check: %v\n%s", checkError, formatted)
	}
}

// fixed is the file at path with every edit of found's fixes applied, from the last to the first, and gofmt'd, with
// how many edits there were. An overlap, no edit at all, or a result gofmt can't format fails the test. The harness
// applies fixes only to hold them to a golden file, and the differences fixture's are held instead by whether they
// type-check, so this stays the port's own.
func fixed(test *testing.T, path string, found []parity.Finding) (formatted []byte, count int) {
	test.Helper()
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	edits := []parity.Edit{}
	for _, finding := range found {
		for _, fix := range finding.Fixes {
			edits = append(edits, fix.Edits...)
		}
	}
	if len(edits) == 0 {
		test.Fatalf("the port made no edit to %s, so the comparison proves nothing", path)
	}
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
	slices.SortFunc(edits, func(first parity.Edit, second parity.Edit) int {
		return cmp.Compare(second.Start, first.Start)
	})
	result := slices.Clone(source)
	for index, edit := range edits {
		if index > 0 && edit.End > edits[index-1].Start {
			test.Fatalf("the port's edits overlap at offset %d", edit.Start)
		}
		result = slices.Concat(result[:edit.Start], []byte(edit.NewText), result[edit.End:])
	}
	formatted, formatError := format.Source(result)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, result)
	}
	return formatted, len(edits)
}
