package redundant_nil_check_on_length

import (
	"cmp"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/simple/s1009"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamPackages is staticcheck's own fixtures for S1009, copied unchanged from simple/s1009/testdata/go1.0 and
// testdata/go1.18, each into testdata/src under its version folder, since both are the folder
// CheckRedundantNilCheckWithLen. The version folder sets the language version of staticcheck's run, which S1009 never
// reads, so the fixtures load from a tree laid out by import path, with the toolchain's version, which the generic
// one needs and the other does not mind. They hold no golden file, since S1009 suggests no fix.
var upstreamPackages = []string{"go1.0/CheckRedundantNilCheckWithLen", "go1.18/CheckRedundantNilCheckWithLen"}

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the query and the
// original's judgments branch on and the near miss of each, and a generated file, which both filter; and shadow,
// which declares a len of its own at package scope.
var parityPackages = []string{"parity", "shadow"}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any other name as readily as under S1009's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1009.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1009.Analyzer.Name)
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
		_, port := parity.Findings(test, s1009.Analyzer, Analyzer, analysistest.TestData(), upstreamPackage)
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
	parity.RequireIdentical(test, s1009.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, parityPackages)...)
}

// The pattern looks through doubled parentheses, a shape the house's testdata cannot hold, since its guard holds
// testdata to gofmt, which folds them to one pair, so the fixture is kept as testdata/unformatted/unformatted.go.txt
// and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1009.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// Over every fixture, each finding's end and every suggested fix, its message and each edit's start, end and new
// text, are the original's, which the parity harness does not compare yet. S1009 suggests no fix, so the ends are what
// this holds, each spanning the whole outer expression, and a fix either side made would be a difference.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, slices.Concat(upstreamPackages, parityPackages, []string{"differences"})...)
	// What the port drops and finds, recorded in the differences fixture, is held by
	// TestPortDiffersFromOriginalOnlyWhereRecorded; every other finding of the original's, the port makes with the
	// same end.
	records := recordsOf(test)
	isRecorded := func(kind string) func(found diagnostic) bool {
		return func(found diagnostic) bool {
			recorded := record{file: found.file, line: found.line, kind: kind, message: found.message}
			return slices.Contains(records, recorded)
		}
	}
	original = slices.DeleteFunc(original, isRecorded("drops"))
	port = slices.DeleteFunc(port, isRecorded("finds"))
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

// The differences fixture holds the shapes where the two could most plausibly part, a bound that is negative or zero
// but no integer, against every comparison the original takes. The port parts from the original there, where the
// original is wrong, each finding recorded on its line: `// port drops: <the original's message>` for a nil check
// that changes the answer, which the original reports, and `// port finds: <message>` for a redundant one it leaves
// alone. The parity harness reads `finds:` and `renames:` alone in this base and refuses a `drops:` (#dcgn32d brings
// it), so this holds the records: the port's findings are the original's, less every drop and with every find, line
// and message, and each record names exactly one finding.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	records := recordsOf(test)
	original, port := parity.Findings(test, s1009.Analyzer, Analyzer, analysistest.TestData(), "differences")
	test.Logf("the original found %d, the port %d, the fixture records %d", len(original), len(port), len(records))
	if len(records) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	// The records name a line, not a column.
	original = withoutColumns(original)
	port = withoutColumns(port)
	drops := []parity.Finding{}
	finds := []parity.Finding{}
	for _, each := range records {
		recorded := parity.Finding{File: each.file, Line: each.line, Message: each.message}
		switch each.kind {
		case "drops":
			if !slices.Contains(original, recorded) {
				test.Errorf("%s records that the port drops it, and the original does not find it", recorded)
			}
			drops = append(drops, recorded)
		case "finds":
			if slices.Contains(original, recorded) {
				test.Errorf("%s records that the port finds it, and the original finds it too", recorded)
			}
			finds = append(finds, recorded)
		}
	}
	expected := slices.Concat(parity.Difference(original, drops), finds)
	for _, missing := range parity.Difference(expected, port) {
		test.Errorf("the port did not find %s, which the original or a record says it does", missing)
	}
	for _, extra := range parity.Difference(port, expected) {
		test.Errorf("the port found %s, which neither the original nor a record accounts for", extra)
	}
}

// withoutColumns is found with every column zero, as a record names its finding.
func withoutColumns(found []parity.Finding) []parity.Finding {
	all := []parity.Finding{}
	for _, each := range found {
		each.Column = 0
		all = append(all, each)
	}
	return all
}

// recordPattern is one record of the differences fixture, the text after its `// port ` opening: `drops:` and the
// message of a finding the original makes and the port does not, or `finds:` and the message of a finding the port
// makes and the original does not.
var recordPattern = regexp.MustCompile(`^(drops|finds): (.+?)\s*$`)

// record is one record of the differences fixture: the file and line it is on, its kind and the message it names.
type record struct {
	file    string
	line    int
	kind    string
	message string
}

// recordsOf reads every `// port ` comment of the differences fixture's .go files, as the parity harness reads its
// records: as the parser finds comments, so a `// port ` inside a string is no record, and a comment it cannot read
// fails.
func recordsOf(test *testing.T) []record {
	test.Helper()
	directory := filepath.Join(analysistest.TestData(), "src", "differences")
	names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
	if globError != nil {
		test.Fatalf("listing %s: %v", directory, globError)
	}
	records := []record{}
	fileSet := token.NewFileSet()
	for _, name := range names {
		file, parseError := parser.ParseFile(fileSet, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatalf("parsing %s: %v", name, parseError)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, text := range strings.Split(comment.Text, "// port ")[1:] {
					match := recordPattern.FindStringSubmatch(text)
					if match == nil {
						test.Fatalf("%s:%d holds a record this test cannot read: %q", filepath.Base(name), line, text)
					}
					records = append(records,
						record{file: filepath.Base(name), line: line, kind: match[1], message: match[2]})
				}
			}
		}
	}
	return records
}

// unformattedTree writes the unformatted fixture out as Go into a tree laid out by import path in a directory of the
// test's own, and returns the tree.
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1009 and the port over them in one
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
		[]*analysis.Analyzer{s1009.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1009.Analyzer:
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
