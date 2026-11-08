package buffer_string_conversion

import (
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1030"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreams are staticcheck's own fixtures for S1030, each copied unchanged, golden file and all, from
// simple/s1030/testdata/go1.0/CheckBytesBufferConversions and go1.9/CheckBytesBufferConversions into testdata/src
// under the same two folders, since the two fixtures share one folder name. The go1.N folder sets the language version
// of staticcheck's run, which S1030 never reads, gating nothing, so the fixtures load from a tree laid out by import
// path as the house's fixtures do, where the aliases of the go1.9 one type-check under the newest language.
var upstreams = []struct {
	pattern string
	file    string
}{
	{pattern: "go1.0/CheckBytesBufferConversions", file: "LintBytesBufferConversions.go"},
	{pattern: "go1.9/CheckBytesBufferConversions", file: "LintBytesBufferConversions.go"},
}

// parityPackages is the fixtures written for the comparison: parity, which holds every shape the query and the
// judgment branch on and the near miss of each, and a generated file, which both filter; parity/shadowed, which
// declares byte and string at package scope; and bytes_test, the path of bytes' external tests, which both skip.
var parityPackages = []string{"parity", "parity/shadowed", "bytes_test"}

// fixtures is every fixture package the tree holds, staticcheck's and the port's own.
var fixtures = []string{
	"go1.0/CheckBytesBufferConversions", "go1.9/CheckBytesBufferConversions", "parity", "parity/shadowed",
	"bytes_test", "differences",
}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any name; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1030.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1030.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		fixture := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstream.pattern))
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("the upstream fixture %s holds no notes, so the comparison proves nothing", upstream.pattern)
		}
		original, port := parity.Findings(test, s1030.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("in %s the fixture expects %d, the original found %d, the port %d", upstream.pattern, len(wants),
			len(original), len(found))
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

// The port's fixes, applied to each upstream fixture, make its golden file, as staticcheck's testutil requires of the
// original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when it holds no
// sections, and the fixed file is gofmt'd before it is compared, which is what aligns the fixed lines' comments. Each
// finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		_, port := diagnosticsOf(test, upstream.pattern)
		path := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstream.pattern), upstream.file)
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
			test.Fatalf("%s's golden file holds %d sections, one a fix's message, which this test does not read",
				upstream.pattern, len(archive.Files))
		}
		edits := []offsetEdit{}
		for _, diagnostic := range port {
			for _, fix := range diagnostic.fixes {
				edits = append(edits, fix.edits...)
			}
		}
		test.Logf("in %s the port found %d, with %d edits", upstream.pattern, len(port), len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", upstream.pattern)
		}
		// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
		slices.SortFunc(edits, func(first offsetEdit, second offsetEdit) int {
			return cmp.Compare(second.start, first.start)
		})
		fixed := slices.Clone(source)
		for index, each := range edits {
			if index > 0 && each.end > edits[index-1].start {
				test.Fatalf("the port's edits to %s overlap at offset %d", upstream.pattern, each.start)
			}
			fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
		}
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes to %s make\n%s\nand the golden file is\n%s", upstream.pattern, formatted,
				archive.Comment)
		}
	}
}

// The original and the port run in one checker over staticcheck's fixtures and the parity fixtures, and their
// findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	upstreamPackages := []string{}
	for _, upstream := range upstreams {
		upstreamPackages = append(upstreamPackages, upstream.pattern)
	}
	parity.RequireIdentical(test, s1030.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstreamPackages, parityPackages)...)
}

// Over every fixture, each finding's end and every suggested fix, its message and each edit's start, end and new
// text, are the original's, which the parity harness does not compare yet. The fix's text is the one the original
// builds through edit.ReplaceWithPattern and the port through edit.ReplaceWithNode, so this is where the two are held
// byte for byte, a receiver in parentheses, across lines and with comments beside it included.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, fixtures...)
	// What the port drops, recorded in the differences fixture, is held by TestPortDropsOnlyWhatIsRecorded; every
	// other finding of the original's, the port makes with the same end and fix.
	drops := recordedDrops(test)
	original = slices.DeleteFunc(original, func(found diagnostic) bool {
		return slices.Contains(drops, parity.Finding{File: found.file, Line: found.line, Message: found.message})
	})
	originalDescribed := describedAll(original)
	portDescribed := describedAll(port)
	test.Logf("the original found %d past the %d drops, the port %d", len(originalDescribed), len(drops),
		len(portDescribed))
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

// The differences fixture holds the shapes where the two could most plausibly part, holders that embed a buffer
// beside methods of its names, receivers across lines and with comments beside them, and conversions nested in
// conversions, calls and function literals. The port parts from the original in one way, where the original is
// wrong: it drops a conversion whose receiver's other method, the one the message asks for, is not the buffer's own,
// each such finding recorded on its line as `// port drops: <the original's message>`. The parity harness reads
// `finds:` and `renames:` alone in this base (#dcgn32d brings `drops:`), so this holds the records: the port finds
// nothing the original does not, and what the original alone finds is exactly what is recorded, line and message,
// with every record matched.
func TestPortDropsOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, s1030.Analyzer, Analyzer, analysistest.TestData(), "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(port) == 0 {
		test.Fatal("the port found nothing in the differences fixture, so the agreement proves nothing")
	}
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's S1030 and the port over them in one
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
		[]*analysis.Analyzer{s1030.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1030.Analyzer:
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
