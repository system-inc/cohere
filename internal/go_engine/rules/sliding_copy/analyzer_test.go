package sliding_copy

import (
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreams are staticcheck's own fixtures for S1018, each copied unchanged, golden file and all, from
// simple/s1018/testdata/go1.0/CheckLoopSlide and go1.18/CheckLoopSlide into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1018 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language.
var upstreams = []struct {
	pattern string
	file    string
}{
	{pattern: "go1.0/CheckLoopSlide", file: "LintLoopSlide.go"},
	{pattern: "go1.18/CheckLoopSlide", file: "generics.go"},
}

// fixtures is every fixture package the tree holds where the original and the port agree, staticcheck's and parity's.
var fixtures = []string{"go1.0/CheckLoopSlide", "go1.18/CheckLoopSlide", "parity"}

// differencesFixtures is the differences fixture's packages: its own, one that declares copy at package scope, and
// one that imports a package under the name copy, the last two scopes where copy names no builtin.
var differencesFixtures = []string{"differences", "differences/shadowed", "differences/imported"}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		fixture := filepath.Join(analysistest.TestData(), "src", upstream.pattern)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("the upstream fixture %s holds no notes, so the comparison proves nothing", upstream.pattern)
		}
		original, port := parity.Findings(test, s1018.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
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
// sections, and the fixed file is gofmt'd before it is compared, which is what drops the blank line the replaced
// loop leaves. Each finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		_, port := diagnosticsOf(test, analysistest.TestData(), upstream.pattern)
		path := filepath.Join(analysistest.TestData(), "src", upstream.pattern, upstream.file)
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
				upstream.file, len(archive.Files))
		}
		edits := []offsetEdit{}
		reported := 0
		for _, diagnostic := range port {
			// Every fix of S1018 replaces the loop its finding is at, so a finding's file is its edits'.
			if diagnostic.file != upstream.file {
				continue
			}
			reported++
			for _, fix := range diagnostic.fixes {
				edits = append(edits, fix.edits...)
			}
		}
		test.Logf("in %s the port found %d, with %d edits", upstream.file, reported, len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", upstream.file)
		}
		// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
		slices.SortFunc(edits, func(first offsetEdit, second offsetEdit) int {
			return cmp.Compare(second.start, first.start)
		})
		fixed := slices.Clone(source)
		for index, each := range edits {
			if index > 0 && each.end > edits[index-1].start {
				test.Fatalf("the port's edits to %s overlap at offset %d", upstream.file, each.start)
			}
			fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
		}
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes to %s make\n%s\nand the golden file is\n%s", upstream.file, formatted,
				archive.Comment)
		}
	}
}

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding every
// shape the query and the judgment branch on and the near miss of each, and a generated file, and their findings must
// be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1018.Analyzer, Analyzer, analysistest.TestData(), "go1.0/CheckLoopSlide",
		"go1.18/CheckLoopSlide", "parity")
}

// Over every fixture where the two agree, each finding's end and every suggested fix, its message and each edit's
// start, end and new text, are the original's, which the parity harness does not compare yet. The original builds its
// fix through code.EditMatch from the identifiers its query binds, and the port through edit.ReplaceWithNode from the
// ones it binds, so this is where the bindings are held to the original's, their parentheses removed, and the loops
// replaced byte for byte, the gofmt-rewritable fixture's included.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, analysistest.TestData(), fixtures...)
	unformattedOriginal, unformattedPort := diagnosticsOf(test, unformattedTree(test), "unformatted")
	originalDescribed := describedAll(slices.Concat(original, unformattedOriginal))
	portDescribed := describedAll(slices.Concat(port, unformattedPort))
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

// Over the differences fixture, the port's findings, with their ends and fixes, are the original's with each record
// applied and nothing else: a finding the original makes where a `drops:` record names it is one the port does not
// make, and a fix the original suggests where a `withholds:` record names it is one the port's same finding leaves
// out. Every record must name exactly one of the original's findings or fixes, and every shape no record is on, the
// counter named copy, a copy declared after the loop, identifiers across lines and nested loops, must be the
// original's to the byte. The parity harness reads `finds:` and `renames:` records alone so far
// (#dcgn32d), so this test holds the port's two kinds, and the port finds and renames nothing the original does not.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, analysistest.TestData(), differencesFixtures...)
	expected := []diagnostic{}
	taken := make([]int, len(records))
	for _, found := range original {
		isDropped := false
		for index, each := range records {
			if each.file != found.file || each.line != found.line {
				continue
			}
			switch each.kind {
			case "drops":
				if each.message == found.message {
					isDropped = true
					taken[index]++
				}
			case "withholds":
				kept := []fix{}
				for _, suggested := range found.fixes {
					if suggested.message == each.message {
						taken[index]++
						continue
					}
					kept = append(kept, suggested)
				}
				found.fixes = kept
			}
		}
		if !isDropped {
			expected = append(expected, found)
		}
	}
	test.Logf("the original found %d, the port %d, the fixture records %d", len(original), len(port), len(records))
	if len(records) == 0 {
		test.Fatal("the differences fixture records nothing, so the comparison proves nothing")
	}
	for index, each := range records {
		if taken[index] != 1 {
			test.Errorf("%s:%d records that the port %s %q, which the original makes %d times there, not once",
				each.file, each.line, each.kind, each.message, taken[index])
		}
	}
	expectedDescribed := describedAll(expected)
	portDescribed := describedAll(port)
	for _, missing := range difference(expectedDescribed, portDescribed) {
		test.Errorf("the original found %s, no record says the port differs, and the port did not", missing)
	}
	for _, extra := range difference(portDescribed, expectedDescribed) {
		test.Errorf("the port found %s, which neither the original nor a record accounts for", extra)
	}
}

// recordPattern is one record of the differences fixture, the text after its `// port ` opening: `drops:` and the
// message of a finding the original makes and the port does not, or `withholds:` and the message of a fix the
// original suggests and the port, making the same finding, leaves out.
var recordPattern = regexp.MustCompile(`^(drops|withholds): (.+?)\s*$`)

// record is one record of the differences fixture: the file and line it is on, its kind and the message it names.
type record struct {
	file    string
	line    int
	kind    string
	message string
}

// recordsOf reads every `// port ` comment of the .go files in the fixture packages patterns name, as the parity
// harness reads its records: as the parser finds comments, so a `// port ` inside a string is no record, and a comment
// it cannot read fails.
func recordsOf(test *testing.T, patterns []string) []record {
	test.Helper()
	records := []record{}
	fileSet := token.NewFileSet()
	for _, pattern := range patterns {
		directory := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern))
		names, globError := filepath.Glob(filepath.Join(directory, "*.go"))
		if globError != nil {
			test.Fatalf("listing %s: %v", directory, globError)
		}
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
							test.Fatalf("%s:%d holds a record this test cannot read: %q", filepath.Base(name), line,
								text)
						}
						records = append(records,
							record{file: filepath.Base(name), line: line, kind: match[1], message: match[2]})
					}
				}
			}
		}
	}
	return records
}

// The pattern looks through parentheses around the loop's condition and doubled ones, and the parser leaves an empty
// statement after a store followed by two semicolons, shapes the house's testdata cannot hold, since its guard holds
// testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a
// tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1018.Analyzer, Analyzer, unformattedTree(test), "unformatted")
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

// diagnosticsOf loads the fixture packages patterns name from the tree at directory, runs staticcheck's S1018 and
// the port over them in one checker, as parity.Findings does, and returns each one's findings with their ends and
// fixes, sorted.
func diagnosticsOf(test *testing.T, directory string, patterns ...string) (original []diagnostic, port []diagnostic) {
	test.Helper()
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
		[]*analysis.Analyzer{s1018.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1018.Analyzer:
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
