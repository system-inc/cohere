package guarded_map_access

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
	"honnef.co/go/tools/simple/s1036"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1036, copied unchanged from
// simple/s1036/testdata/go1.0/CheckUnnecessaryGuard into testdata/src/CheckUnnecessaryGuard with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which S1036 never reads, so the fixture loads from a
// tree laid out by import path as the house's fixtures do.
const upstream = "CheckUnnecessaryGuard"

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the query branches on
// and the near miss of each, with a generated file, which neither filters; and shadow, which declares an append of
// its own at package scope.
var parityPackages = []string{"parity", "shadow"}

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
	_, port := diagnosticsOf(test, upstream)
	path := filepath.Join(analysistest.TestData(), "src", upstream, "LintUnnecessaryGuard.go")
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
	edits := []offsetEdit{}
	for _, diagnostic := range port {
		for _, fix := range diagnostic.fixes {
			edits = append(edits, fix.edits...)
		}
	}
	test.Logf("the port found %d, with %d edits", len(port), len(edits))
	if len(edits) == 0 {
		test.Fatal("the port made no edit to the upstream fixture, so the comparison proves nothing")
	}
	formatted, formatError := format.Source(applied(test, source, edits))
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v", formatError)
	}
	if !bytes.Equal(formatted, archive.Comment) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", formatted, archive.Comment)
	}
}

// The original and the port run in one checker over staticcheck's fixture and the parity fixtures, and their
// findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1036.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat([]string{upstream}, parityPackages)...)
}

// The shapes gofmt rewrites, a condition in parentheses, which gofmt strips and the pattern looks through, and
// doubled parentheses elsewhere, are held the same way. The guard holds every .go file of the tree to gofmt, so the
// fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own.
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

// Over every fixture, each finding's end and every suggested fix, its message and each edit's start, end and new
// text, are the original's, which the parity harness does not compare yet. The fix's text is format.Node of the if's
// one body statement on both sides, so this is where the two are held byte for byte, a split call with comments
// among its arguments included. What the differences fixture records the port drops is left out of the original's
// side, and a finding it records the port moves is compared without its fixes, both held by the tests below.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, slices.Concat([]string{upstream}, parityPackages, []string{"differences"})...)
	drops, moves := recordedDifferences(test)
	original = slices.DeleteFunc(original, func(found diagnostic) bool {
		return slices.Contains(drops, findingOf(found))
	})
	withoutMovedFixes := func(found []diagnostic) []string {
		all := []string{}
		for _, each := range found {
			if slices.Contains(moves, findingOf(each)) {
				each.fixes = nil
			}
			all = append(all, described(each))
		}
		return all
	}
	originalDescribed := withoutMovedFixes(original)
	portDescribed := withoutMovedFixes(port)
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

// The differences fixture holds the ways the port parts from the original, where the original is wrong, and the
// near shapes where the two agree. It drops a guard the zero value does not cover: an append that spreads its last
// argument against a literal that holds that argument as one element, and an append of nothing against an empty
// literal, which is nil where the literal is not, each such finding recorded on its line as `// port drops: <the
// original's message>`. The parity harness reads `finds:` and `renames:` alone in this base (#dcgn32d brings
// `drops:` and `moves:`), so this holds the records: the port finds nothing the original does not, and what the
// original alone finds is exactly what is recorded, line and message, with every record matched.
func TestPortDropsOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1036.Analyzer, Analyzer, analysistest.TestData(), "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	for _, extra := range parity.Difference(port, original) {
		test.Errorf("the port alone found %s, which the fixture does not record", extra)
	}
	drops, _ := recordedDifferences(test)
	if len(drops) == 0 {
		test.Fatal("the differences fixture records no drop, so the comparison proves nothing")
	}
	originalAlone := []parity.Finding{}
	for _, found := range parity.Difference(original, port) {
		// The records name a line, not a column.
		found.Column = 0
		originalAlone = append(originalAlone, found)
	}
	for _, missed := range parity.Difference(originalAlone, drops) {
		test.Errorf("the original found %s, and the port did not, a regression", missed)
	}
	for _, unmatched := range parity.Difference(drops, originalAlone) {
		test.Errorf("the fixture records that the port drops %s, and the original does not find it", unmatched)
	}
}

// The other way the port parts from the original is the fix of a guard that is another if's else branch, where the
// original's bare statement, `} else m[k]++`, does not parse, and the port writes the if's body, a block, each such
// finding recorded on its line as `// port moves: <the message>`. Each record is one finding both sides make at the
// same range, whose fixes differ; the original's edit, applied alone to the fixture, makes a file that does not
// parse, and the port's makes one that type-checks. Every other fix of the fixture is the original's, which
// TestPortFixesAndRangesMatchOriginal holds.
func TestPortMovesOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "differences")
	_, moves := recordedDifferences(test)
	if len(moves) == 0 {
		test.Fatal("the differences fixture records no move, so the comparison proves nothing")
	}
	test.Logf("the fixture records %d moves", len(moves))
	for _, move := range moves {
		originalFound, isOriginalFound := soleDiagnosticAt(original, move)
		portFound, isPortFound := soleDiagnosticAt(port, move)
		if !isOriginalFound || !isPortFound {
			test.Errorf("the fixture records that the port moves %s, and the two do not each find it once", move)
			continue
		}
		if described(originalFound) == described(portFound) {
			test.Errorf("the fixture records that the port moves %s, and its fix is the original's", move)
			continue
		}
		source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "differences", move.File))
		if readError != nil {
			test.Fatal(readError)
		}
		if _, parseError := parser.ParseFile(token.NewFileSet(), move.File,
			applied(test, source, editsOf(originalFound)), parser.SkipObjectResolution); parseError == nil {
			test.Errorf("the original's fix at %s parses, so the record moves nothing", move)
		}
		if checkError := typeChecks(move.File, applied(test, source, editsOf(portFound))); checkError != nil {
			test.Errorf("the port's fix at %s does not type-check: %v", move, checkError)
		}
	}
}

// Every fix the port suggests in the differences fixture, applied together, makes a file that type-checks, and the
// original's, applied together, make one that does not, as each recorded move says, so a move that fixed nothing
// broken, or a port fix that breaks what it touches, fails here.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "differences")
	name := "differences.go"
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "src", "differences", name))
	if readError != nil {
		test.Fatal(readError)
	}
	portEdits := []offsetEdit{}
	for _, found := range port {
		portEdits = append(portEdits, editsOf(found)...)
	}
	originalEdits := []offsetEdit{}
	for _, found := range original {
		originalEdits = append(originalEdits, editsOf(found)...)
	}
	test.Logf("the port makes %d edits, the original %d", len(portEdits), len(originalEdits))
	if len(portEdits) == 0 {
		test.Fatal("the port made no edit to the differences fixture, so the comparison proves nothing")
	}
	if checkError := typeChecks(name, applied(test, source, portEdits)); checkError != nil {
		test.Errorf("the port's fixes make code that does not type-check: %v", checkError)
	}
	if checkError := typeChecks(name, applied(test, source, originalEdits)); checkError == nil {
		test.Error("the original's fixes to the differences fixture type-check, so no move fixes anything")
	}
}

// recordedDifferences is every `// port drops: <message>` and `// port moves: <message>` record of the differences
// fixture, each on its comment's line, in its file's base name, with no column, as the parity harness reads its own
// records. A `// port ` comment that is neither fails, since this fixture records nothing else.
func recordedDifferences(test *testing.T) (drops []parity.Finding, moves []parity.Finding) {
	test.Helper()
	directory := filepath.Join(analysistest.TestData(), "src", "differences")
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing the differences fixture: %v", globError)
	}
	fileSet := token.NewFileSet()
	for _, name := range names {
		source, readError := os.ReadFile(name)
		if readError != nil {
			test.Fatal(readError)
		}
		lines := strings.Split(string(source), "\n")
		file, parseError := parser.ParseFile(fileSet, name, source, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing the differences fixture: %v", parseError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, record := range strings.Split(comment.Text, "// port ")[1:] {
					record = strings.TrimSpace(record)
					at := parity.Finding{File: filepath.Base(name), Line: line}
					// Each record carries its reason in a comment on the line above it.
					if line < 2 || !strings.HasPrefix(strings.TrimSpace(lines[line-2]), "//") {
						test.Fatalf("%s:%d records %q with no reason on the line above", at.File, line, record)
					}
					if message, isDrop := strings.CutPrefix(record, "drops: "); isDrop {
						at.Message = message
						drops = append(drops, at)
						continue
					}
					if message, isMove := strings.CutPrefix(record, "moves: "); isMove {
						at.Message = message
						moves = append(moves, at)
						continue
					}
					test.Fatalf("%s:%d holds a record that is no drop and no move: %q", at.File, line, record)
				}
			}
		}
	}
	return drops, moves
}

// findingOf is found as the records name it: its file, line and message, with no column.
func findingOf(found diagnostic) parity.Finding {
	return parity.Finding{File: found.file, Line: found.line, Message: found.message}
}

// soleDiagnosticAt is the one diagnostic of found that at names, and false when there is none or more than one.
func soleDiagnosticAt(found []diagnostic, at parity.Finding) (diagnostic, bool) {
	matching := slices.DeleteFunc(slices.Clone(found), func(each diagnostic) bool {
		return findingOf(each) != at
	})
	if len(matching) != 1 {
		return diagnostic{}, false
	}
	return matching[0], true
}

// editsOf is every edit of every fix found suggests.
func editsOf(found diagnostic) []offsetEdit {
	edits := []offsetEdit{}
	for _, each := range found.fixes {
		edits = append(edits, each.edits...)
	}
	return edits
}

// applied is source with edits made, from the last to the first, so each edit's offsets still name the bytes they
// were read against. Edits that overlap fail the test.
func applied(test *testing.T, source []byte, edits []offsetEdit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(first offsetEdit, second offsetEdit) int {
		return cmp.Compare(second.start, first.start)
	})
	fixed := slices.Clone(source)
	for index, each := range ordered {
		if index > 0 && each.end > ordered[index-1].start {
			test.Fatalf("the edits overlap at offset %d", each.start)
		}
		fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
	}
	return fixed
}

// typeChecks parses source as the one file of a package and type-checks it, and is the first error either finds.
// The differences fixture imports nothing, so the package needs no importer.
func typeChecks(name string, source []byte) error {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
	if parseError != nil {
		return parseError
	}
	_, checkError := (&types.Config{}).Check("differences", fileSet, []*ast.File{file}, nil)
	return checkError
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1036 and the port over them in one
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
		[]*analysis.Analyzer{s1036.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1036.Analyzer:
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
