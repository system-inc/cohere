package separate_declaration_and_assignment

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1021"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
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
	fixture := filepath.Join(analysistest.TestData(), "src", upstream)
	wants, readError := staticcheck_wants.Read(fixture)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixture holds no notes, so the comparison proves nothing")
	}
	_, port := parity.Findings(test, s1021.Analyzer, Analyzer, analysistest.TestData(), upstream)
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
	_, port := diagnosticsOf(test, upstream)
	path := filepath.Join(analysistest.TestData(), "src", upstream, "LintDeclareAssign.go")
	golden, goldenError := os.ReadFile(path + ".golden")
	if goldenError != nil {
		test.Fatal(goldenError)
	}
	archive := txtar.Parse(golden)
	if len(archive.Files) > 0 {
		test.Fatalf("the golden file holds %d sections, one a fix's message, which this test does not read",
			len(archive.Files))
	}
	formatted, edits := fixed(test, path, port)
	test.Logf("the port found %d, with %d edits", len(port), edits)
	if !bytes.Equal(formatted, archive.Comment) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", formatted, archive.Comment)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the check branches on and the near miss of each, and a generated file, and their findings must be the same
// multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1021.Analyzer, Analyzer, analysistest.TestData(), identical...)
}

// Over every fixture where the two agree, each finding's end and every suggested fix, its message and each edit's
// start, end and new text, are the original's, which the parity harness does not compare yet. Both build the fix's
// declaration from the same nodes and print it through edit.ReplaceWithNode, so this is where the text is held byte
// for byte, a value across lines, a composite literal and comments before, inside and after the value among them.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, identical...)
	originalDescribed := describedAll(original)
	portDescribed := describedAll(port)
	test.Logf("the original found %d, the port %d", len(originalDescribed), len(portDescribed))
	if len(originalDescribed) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, missing := range difference(originalDescribed, portDescribed) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range difference(portDescribed, originalDescribed) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
}

// The differences fixture records each declaration in a switch's or a select's clause, which the port alone finds,
// and holds the clause shapes both leave alone and the nested block both find, and nothing the original finds is
// missing from the port's.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1021.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// In the differences fixture, every finding the original makes the port makes with the same end and fixes, and the
// port's fixes, its own findings' among them, applied together, make a file gofmt formats and the type checker
// accepts, so a clause's merged declaration is as sound as a block's.
func TestPortFixesInClausesCompile(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "differences")
	portDescribed := describedAll(port)
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(port) <= len(original) {
		test.Fatal("the port found nothing the original did not, so the clauses' fixes go unproven")
	}
	for _, missing := range difference(describedAll(original), portDescribed) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
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
// how many edits there were. An overlap, no edit at all, or a result gofmt can't format fails the test.
func fixed(test *testing.T, path string, found []diagnostic) (formatted []byte, count int) {
	test.Helper()
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	edits := []offsetEdit{}
	for _, each := range found {
		for _, suggested := range each.fixes {
			edits = append(edits, suggested.edits...)
		}
	}
	if len(edits) == 0 {
		test.Fatalf("the port made no edit to %s, so the comparison proves nothing", path)
	}
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
	slices.SortFunc(edits, func(first offsetEdit, second offsetEdit) int {
		return cmp.Compare(second.start, first.start)
	})
	result := slices.Clone(source)
	for index, each := range edits {
		if index > 0 && each.end > edits[index-1].start {
			test.Fatalf("the port's edits overlap at offset %d", each.start)
		}
		result = slices.Concat(result[:each.start], []byte(each.newText), result[each.end:])
	}
	formatted, formatError := format.Source(result)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, result)
	}
	return formatted, len(edits)
}

// diagnostic is one finding as the fixes test compares it: where it starts and ends, its message and its fixes.
type diagnostic struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
	fixes     []fix
}

// fix is one suggested fix, its message and its edits.
type fix struct {
	message string
	edits   []offsetEdit
}

// offsetEdit is one edit of a fix, by byte offsets in its file, so it applies to the file's bytes.
type offsetEdit struct {
	start   int
	end     int
	newText string
}

// described is found as one line, with where it ends and every fix and edit it suggests.
func described(found diagnostic) string {
	text := &strings.Builder{}
	fmt.Fprintf(text, "%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
	for _, each := range found.fixes {
		fmt.Fprintf(text, " fix %q", each.message)
		for _, edit := range each.edits {
			fmt.Fprintf(text, " [%d,%d) %q", edit.start, edit.end, edit.newText)
		}
	}
	return text.String()
}

// describedAll is every finding of found, described.
func describedAll(found []diagnostic) []string {
	all := []string{}
	for _, each := range found {
		all = append(all, described(each))
	}
	return all
}

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1021 and the port over them in one
// checker, as parity.Findings does, and returns each one's findings with their ends and fixes, sorted.
func diagnosticsOf(test *testing.T, patterns ...string) (original []diagnostic, port []diagnostic) {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{s1021.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v: %v", patterns, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, reported := range action.Diagnostics {
			position := action.Package.Fset.Position(reported.Pos)
			end := action.Package.Fset.Position(reported.End)
			found := diagnostic{
				file:      filepath.Base(position.Filename),
				line:      position.Line,
				column:    position.Column,
				endLine:   end.Line,
				endColumn: end.Column,
				message:   reported.Message,
			}
			for _, suggested := range reported.SuggestedFixes {
				each := fix{message: suggested.Message}
				for _, edit := range suggested.TextEdits {
					each.edits = append(each.edits, offsetEdit{
						start:   action.Package.Fset.Position(edit.Pos).Offset,
						end:     action.Package.Fset.Position(edit.End).Offset,
						newText: string(edit.NewText),
					})
				}
				found.fixes = append(found.fixes, each)
			}
			switch action.Analyzer {
			case s1021.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first diagnostic, second diagnostic) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []string, without []string) []string {
	remaining := map[string]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []string{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}
