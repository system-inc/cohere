package unnecessary_blank_identifier

import (
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1005"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1005, which it keeps under simple/s1005/testdata/go1.0, go1.3 and go1.4,
// each copied unchanged, golden files and all, into the module of its folder's name. S1005 reports a range statement
// only where the language is go1.4 or newer, so the version matters: staticcheck's testutil loads each folder as a
// module whose go.mod, overlaid, says `go 1.N` for its folder's name, and the go line is what its loader and ours
// alike hand the type checker as the package's version, which the gate reads. So testdata/src/go1.0, go1.3 and go1.4
// are each a module, example.com as staticcheck's is, whose go.mod says its folder's version, and every load checks
// that the version reached the package, so the go1.3 fixture's silence is the gate's and not a version lost.
const upstream = "CheckUnnecessaryBlank"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's three.
var versions = []string{"go1.0", "go1.3", "go1.4"}

// goldens are the upstream files with a golden file beside them, by the version of the module that holds them.
var goldens = []struct {
	version string
	file    string
}{
	{version: "go1.0", file: "LintBlankOK.go"},
	{version: "go1.0", file: "receive-blank.go"},
	{version: "go1.4", file: "range.go"},
}

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched, as the parity
// harness loads a fixture directory that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.3 fixture holds no note, so there
// the original must report nothing too, as staticcheck's own test requires of it, and the ranges it holds are the ones
// the go1.4 fixture's notes expect, so the gate is what parts them.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wanted := 0
	for _, version := range versions {
		fixture := filepath.Join(module(version), upstream)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		wanted += len(wants)
		// The load checks the package's version, and parity.Findings below loads the same module the same way.
		diagnosticsOf(test, module(version), version, "./"+upstream)
		original, port := parity.Findings(test, s1005.Analyzer, Analyzer, module(version), "./"+upstream)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("under %s the fixture expects %d, the original found %d, the port %d", version, len(wants),
			len(original), len(found))
		if len(wants) == 0 && len(original) != 0 {
			test.Errorf("under %s the fixture expects nothing, and the original found %v, so the version did not "+
				"reach it as staticcheck's testutil hands it over", version, original)
		}
		unexpected, untaken := staticcheck_wants.Compare(wants, found)
		for _, extra := range unexpected {
			test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
				extra.Message)
		}
		for _, missing := range untaken {
			test.Errorf("the port reported nothing matching %s", missing)
		}
	}
	if wanted == 0 {
		test.Fatal("the upstream fixtures hold no notes, so the comparison proves nothing")
	}
}

// The port's fixes, applied to each upstream file that has a golden file, make it, as staticcheck's testutil requires
// of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when it holds
// no sections, and the fixed file is gofmt'd before it is compared. The go1.3 fixture has no golden file, and no fix.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, golden := range goldens {
		_, port := diagnosticsOf(test, module(golden.version), golden.version, "./"+upstream)
		path := filepath.Join(module(golden.version), upstream, golden.file)
		source, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		goldenSource, goldenError := os.ReadFile(path + ".golden")
		if goldenError != nil {
			test.Fatal(goldenError)
		}
		archive := txtar.Parse(goldenSource)
		if len(archive.Files) > 0 {
			test.Fatalf("%s's golden file holds %d sections, one a fix's message, which this test does not read",
				golden.file, len(archive.Files))
		}
		edits := []offsetEdit{}
		reported := 0
		for _, diagnostic := range port {
			// Every fix of S1005 edits the file its finding is in, so a finding's file is its edits'.
			if diagnostic.file != golden.file {
				continue
			}
			reported++
			for _, fix := range diagnostic.fixes {
				edits = append(edits, fix.edits...)
			}
		}
		test.Logf("under %s in %s the port found %d, with %d edits", golden.version, golden.file, reported,
			len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", golden.file)
		}
		// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
		slices.SortFunc(edits, func(first offsetEdit, second offsetEdit) int {
			return cmp.Compare(second.start, first.start)
		})
		fixed := slices.Clone(source)
		for index, each := range edits {
			if index > 0 && each.end > edits[index-1].start {
				test.Fatalf("the port's edits to %s overlap at offset %d", golden.file, each.start)
			}
			fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
		}
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes to %s make\n%s\nand the golden file is\n%s", golden.file, formatted,
				archive.Comment)
		}
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, under go1.4 and go1.3, and
// over staticcheck's under go1.0. Parity's holds every shape the queries and the range branches on and the near miss
// of each, a generated file, and files tagged newer than the module, and their findings must be the same multiset of
// file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.4"), "./"+upstream, "./parity")
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.3"), "./"+upstream, "./parity")
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.0"), "./"+upstream)
}

// Over every fixture of every version, each finding's end and every suggested fix, its message and each edit's start,
// end and new text, are the original's, which the parity harness does not compare yet. The receive fixes' text is
// printed from the syntax each side builds, the original through its pattern's binding and its copy of the
// assignment and the port through its own, so this is where the two are held byte for byte, comments inside and
// beside the assignment and right sides across lines among them.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original := []diagnostic{}
	port := []diagnostic{}
	for _, version := range versions {
		originalOfVersion, portOfVersion := diagnosticsOf(test, module(version), version, "./...")
		original = append(original, originalOfVersion...)
		port = append(port, portOfVersion...)
	}
	requireSameFixesAndRanges(test, original, port)
}

// The shapes gofmt rewrites, spacing inside the deleted ranges, no spacing around an assignment's token, and doubled
// parentheses around a receive and a blank, are held the same way, ends and fixes included. The guard holds every .go
// file of the tree to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go
// into a go1.4 module of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join(analysistest.TestData(), "unformatted", "unformatted.go.txt"))
	if readError != nil {
		test.Fatal(readError)
	}
	directory := test.TempDir()
	if writeError := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com\n\ngo 1.4\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	packageDirectory := filepath.Join(directory, "unformatted")
	if makeError := os.MkdirAll(packageDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(packageDirectory, "unformatted.go"), source, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, directory, "./unformatted")
	original, port := diagnosticsOf(test, directory, "go1.4", "./unformatted")
	requireSameFixesAndRanges(test, original, port)
}

// The differences fixture holds the shapes where the two could most plausibly part, parenthesized blanks in a range
// and in an assignment, a definition's two-byte token, a receive-only channel, a labeled receive, both left sides
// blank, and ranges over an alias of a function and over a named map, and records none, since the port parts from
// the original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two
// are required to agree there finding for finding, which fails when the original finds nothing, so the agreement is
// over findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1005.Analyzer, Analyzer, module("go1.4"), "./differences")
}

// requireSameFixesAndRanges fails test unless original and port are the same findings, ends and fixes included, and
// unless both of the original's fix messages are among them, since the receive alone and the rest fix differently.
func requireSameFixesAndRanges(test *testing.T, original []diagnostic, port []diagnostic) {
	test.Helper()
	originalDescribed := describedAll(original)
	portDescribed := describedAll(port)
	test.Logf("the original found %d, the port %d", len(originalDescribed), len(portDescribed))
	if len(originalDescribed) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	messages := map[string]int{}
	for _, each := range original {
		for _, suggested := range each.fixes {
			messages[suggested.message]++
		}
	}
	test.Logf("the original's fixes by message: %v", messages)
	for _, message := range []string{"Remove assignment to blank identifier", "Simplify channel receive operation"} {
		if messages[message] == 0 {
			test.Errorf("the original suggested no fix %q, so the fixture does not hold its branch", message)
		}
	}
	for _, missing := range difference(originalDescribed, portDescribed) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range difference(portDescribed, originalDescribed) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
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

// diagnosticsOf loads the packages patterns name from the fixture module in directory, runs staticcheck's S1005 and
// the port over them in one checker, as parity.Findings does, and returns each one's findings with their ends and
// fixes, sorted. Every package loaded must carry version as its own, the go line its module's go.mod says, which is
// what both analyzers' version gates read.
func diagnosticsOf(
	test *testing.T,
	directory string,
	version string,
	patterns ...string,
) (original []diagnostic, port []diagnostic) {
	test.Helper()
	loaded, loadError := load.Packages(directory, patterns, moduleEnvironment, nil)
	if loadError != nil {
		test.Fatalf("loading %v under %s: %v", patterns, version, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v under %s found no package, so the comparison would prove nothing", patterns, version)
	}
	for _, root := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
		}
		if root.Types.GoVersion() != version {
			test.Fatalf("fixture package %s loaded under %q, where its module says %s", root.ID,
				root.Types.GoVersion(), version)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{s1005.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v under %s: %v", patterns, version, analyzeError)
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
			case s1005.Analyzer:
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
