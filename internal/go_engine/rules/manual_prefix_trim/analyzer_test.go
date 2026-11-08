package manual_prefix_trim

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1017, copied unchanged from simple/s1017/testdata/go1.0/CheckTrim into
// testdata/src/CheckTrim. The go1.0 folder sets the language version of staticcheck's run, which S1017 never reads, so
// the fixture loads from a tree laid out by import path as the house's fixtures do. It holds no golden file, since
// S1017 suggests no fix.
const upstream = "CheckTrim"

// parityFixtures is the packages parity's comparison runs over beside upstream: parity, which holds every shape the
// check branches on and the near miss of each, a package spelled strings at another path and a generated file, and
// packagelength, whose package scope declares a len of its own.
var parityFixtures = []string{"parity", "packagelength"}

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
	_, port := parity.Findings(test, s1017.Analyzer, Analyzer, analysistest.TestData(), upstream)
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

// The original and the port run in one checker over staticcheck's fixture and parity's, and their findings must be
// the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1017.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat([]string{upstream}, parityFixtures)...)
}

// gofmt strips the parentheses around an if statement's condition, which the original takes as written, so a
// parenthesized guard is held to the original's judgment here. The guard holds every .go file of the tree to gofmt,
// so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's
// own.
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
	parity.RequireIdentical(test, s1017.Analyzer, Analyzer, directory, "unformatted")
}

// Over every fixture, each finding's end and every suggested fix, its message and each edit's start, end and new
// text, are the original's, which the parity harness does not compare yet. S1017 suggests no fix, so the ends are
// what this holds, the whole if statement for a guarded call and its header for a guarded slice, and a fix either
// side made would be a difference.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, slices.Concat([]string{upstream}, parityFixtures, []string{"differences"})...)
	// What the port drops, recorded in the differences fixture, is held by TestPortDropsOnlyWhatIsRecorded; every
	// other finding of the original's, the port makes with the same end.
	drops := recordedDrops(test)
	original = slices.DeleteFunc(original, func(found diagnostic) bool {
		return slices.Contains(drops, parity.Finding{File: found.file, Line: found.line, Message: found.message})
	})
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

// The differences fixture holds the shapes where the two could most plausibly part, a type parameter of byte slices,
// a promoted field against its embedding's, literal offsets of float and rune kinds, else if chains around
// initializers, guards labeled and in clauses and literals, a dot import, and indexes of conversions, len and cap.
// The port parts from the original in one way, where the original is wrong (#zx3fz6w): it drops a guard on a
// selector or index whose syntax holds a call or a receive, each such finding recorded on its line as
// `// port drops: <the original's message>`. The parity harness reads `finds:` and `renames:` alone in this base
// (#dcgn32d brings `drops:`), so this holds the records: the port finds nothing the original does not, and what the
// original alone finds is exactly what is recorded, line and message, with every record matched.
func TestPortDropsOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1017.Analyzer, Analyzer, analysistest.TestData(), "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	for _, extra := range parity.Difference(port, original) {
		test.Errorf("the port alone found %s, which the fixture does not record", extra)
	}
	drops := recordedDrops(test)
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

// recordedDrops is every `// port drops: <message>` record of the differences fixture, each on its comment's line,
// in its file's base name, with no column, as the parity harness reads its own records. A `// port ` comment that is
// no drop fails, since this fixture records nothing else.
func recordedDrops(test *testing.T) []parity.Finding {
	test.Helper()
	directory := filepath.Join(analysistest.TestData(), "src", "differences")
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing the differences fixture: %v", globError)
	}
	drops := []parity.Finding{}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing the differences fixture: %v", parseError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, record := range strings.Split(comment.Text, "// port ")[1:] {
					message, isDrop := strings.CutPrefix(strings.TrimSpace(record), "drops: ")
					if !isDrop {
						test.Fatalf("%s:%d holds a record that is no drop: %q", filepath.Base(name), line, record)
					}
					drops = append(drops, parity.Finding{File: filepath.Base(name), Line: line, Message: message})
				}
			}
		}
	}
	return drops
}

// The one place the original crashes and the port declines: astutil.Equal panics on a node kind it does not know, a
// function type in an index, and on a field's tag absent on one side only, and a panic in a check is a crash under
// its rule's name. The original can't run over the panicking fixture, so the port runs alone, through analysistest,
// which fails on any finding there, as the fixture holds no want, and on the panic.
func TestPortDeclinesWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "panicking")
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1017 and the port over them in one
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
		[]*analysis.Analyzer{s1017.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1017.Analyzer:
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
