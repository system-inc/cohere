package redundant_make_arguments

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1019"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamPackages is staticcheck's own fixtures for S1019, copied unchanged from simple/s1019/testdata/go1.0 and
// testdata/go1.18, each into testdata/src under its version folder, since both are the folder CheckMakeLenCap. The
// version folder sets the language version of staticcheck's run, which S1019 never reads, so the fixtures load from
// a tree laid out by import path, with the toolchain's version, which the generic one needs and the other does not
// mind. They hold no golden file, since S1019 suggests no fix.
var upstreamPackages = []string{"go1.0/CheckMakeLenCap", "go1.18/CheckMakeLenCap"}

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the two queries branch
// on and the near miss of each, a generated file, which both filter, and a map_test.go outside the runtime; shadow,
// which declares a make of its own at package scope; and runtime_test, the package whose map_test.go the original
// skips, with line directives that name that file and another.
var parityPackages = []string{"parity", "shadow", "runtime_test"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under S1018's name, or any other, as readily as under S1019's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1019.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1019.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstreamPackage := range upstreamPackages {
		fixture := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstreamPackage))
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("%s holds no notes, so the comparison proves nothing", upstreamPackage)
		}
		_, port := parity.Findings(test, s1019.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("%s expects %d, the port found %d", upstreamPackage, len(wants), len(found))
		unexpected, untaken := staticcheck_wants.Compare(wants, found)
		for _, extra := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
				extra.Message)
		}
		for _, missing := range untaken {
			test.Errorf("the port reported nothing matching %s", missing)
		}
	}
}

// The original and the port run in one checker over staticcheck's fixtures and the parity fixtures, and their
// findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1019.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, parityPackages)...)
}

// Over every fixture, each finding's end and every suggested fix, its message and each edit's start, end and new
// text, are the original's, which the parity harness does not compare yet. S1019 suggests no fix, so the ends are what
// this holds, each spanning the argument make defaults with its parentheses removed, and a fix either side made would
// be a difference.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, slices.Concat(upstreamPackages, parityPackages, []string{"differences"})...)
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

// The differences fixture holds the shapes where the two could most plausibly part, an alias and a type declared in a
// function, channels of a type parameter's element, a named constraint whose channels differ in direction, calls
// nested in calls and function literals, a package variable's initializer, a method named make, and sizes read anew
// each time. The port parts from the original in one way, where the original is wrong: it drops a length that
// receives from a channel, each such finding recorded on its line as `// port drops: <the original's message>`. The
// parity harness reads `finds:` and `renames:` alone in this base (#dcgn32d brings `drops:`), so this holds the
// records: the port finds nothing the original does not, and what the original alone finds is exactly what is
// recorded, line and message, with every record matched.
func TestPortDropsOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1019.Analyzer, Analyzer, analysistest.TestData(), "differences")
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1019 and the port over them in one
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
		[]*analysis.Analyzer{s1019.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1019.Analyzer:
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
