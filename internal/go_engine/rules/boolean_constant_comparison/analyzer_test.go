package boolean_constant_comparison

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
	"honnef.co/go/tools/simple/s1002"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreams are staticcheck's own fixtures for S1002, each copied unchanged, golden file and all, from
// simple/s1002/testdata/go1.0/CheckIfBoolCmp and go1.18/CheckIfBoolCmp into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1002 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language. The first holds a test file, which the
// check leaves alone.
var upstreams = []struct {
	pattern string
	file    string
}{
	{pattern: "go1.0/CheckIfBoolCmp", file: "bool-cmp.go"},
	{pattern: "go1.18/CheckIfBoolCmp", file: "bool-cmp_generics.go"},
}

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's two and parity's, which imports a package of constants.
var fixtures = []string{"go1.0/CheckIfBoolCmp", "go1.18/CheckIfBoolCmp", "parity"}

// differencesFixtures is the differences fixture's one package, where the two make the same findings and part on
// some fixes.
var differencesFixtures = []string{"differences"}

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
		original, port := diagnosticsOf(test, analysistest.TestData(), upstream.pattern)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.file), Line: each.line, Message: each.message,
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
// sections, and the fixed file is gofmt'd before it is compared. Each finding suggests one fix, and neither golden
// file holds sections.
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
		edits := editsIn(port, upstream.file)
		test.Logf("in %s the port found %d, with %d edits", upstream.file, len(port), len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", upstream.file)
		}
		fixed := applied(test, upstream.file, source, edits)
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

// The original and the port run in one checker over staticcheck's fixtures, parity's, which holds every shape the
// judgment branches on and the near miss of each, a generated file, a test file, line directives, a dot import and a
// package of constants, and the differences fixture, whose findings the two share and whose fixes alone part, and
// their findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1002.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(fixtures, differencesFixtures)...)
}

// Doubled parentheses around either side and comparisons written without spaces are shapes the house's testdata
// cannot hold, since its guard holds testdata to gofmt, so the fixture is kept as
// testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1002.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// Over every fixture where the two agree, each finding's end and every suggested fix, its message and each edit's
// start, end and new text, are the original's, which the parity harness does not compare yet. Both write the fix
// through edit.ReplaceWithString from the other side's render, so this is where the render and the folding of
// negations are held byte for byte, across lines, with comments among them, and in the gofmt-rewritable fixture.
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
// applied and nothing else: a finding where a `moves:` record names it is the original's at the same range with the
// same fix, its edit over the same bytes, and its new text other than the original's. Every record must name exactly
// one of the original's findings, and every shape no record is on must be the original's to the byte. The parity
// harness reads `finds:` and `renames:` records alone so far (#dcgn32d), so this test holds the port's moves, and the
// port finds, drops, renames and withholds nothing.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, analysistest.TestData(), differencesFixtures...)
	isMovedAt := map[string]bool{}
	expectedDescribed := []string{}
	taken := make([]int, len(records))
	for _, found := range original {
		isMoved := false
		for index, each := range records {
			if each.file == found.file && each.line == found.line && each.message == found.message {
				isMoved = true
				taken[index]++
			}
		}
		if isMoved {
			isMovedAt[placeOf(found)] = true
			expectedDescribed = append(expectedDescribed, describedWithoutText(found))
			continue
		}
		expectedDescribed = append(expectedDescribed, described(found))
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
	portDescribed := []string{}
	for _, found := range port {
		if isMovedAt[placeOf(found)] {
			portDescribed = append(portDescribed, describedWithoutText(found))
			continue
		}
		portDescribed = append(portDescribed, described(found))
	}
	for _, missing := range difference(expectedDescribed, portDescribed) {
		test.Errorf("the original found %s, no record says the port differs, and the port did not", missing)
	}
	for _, extra := range difference(portDescribed, expectedDescribed) {
		test.Errorf("the port found %s, which neither the original nor a record accounts for", extra)
	}
	// A move that writes what the original writes moves nothing.
	for _, originalFound := range original {
		if !isMovedAt[placeOf(originalFound)] {
			continue
		}
		for _, portFound := range port {
			if placeOf(portFound) == placeOf(originalFound) && described(portFound) == described(originalFound) {
				test.Errorf("%s is recorded as moved, and the port's fix is the original's", placeOf(portFound))
			}
		}
	}
}

// Applied to the differences fixture, the port's fixes make a package that type-checks, and the original's make one
// that does not, with an error on every line a record is on: the original negates an operand no negation takes, and
// the port negates the comparison whole. The fixed files are loaded through an overlay over the fixtures on disk,
// which changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, analysistest.TestData(), differencesFixtures...)
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	for _, each := range records {
		// A fix replaces the comparison on its own line with text of one line, so the fixed file's lines are the
		// fixture's, and an error the original's fix makes is on the line of its record.
		place := fmt.Sprintf("%c%s:%d:", filepath.Separator, each.file, each.line)
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, place)
		})
		if !isBroken {
			test.Errorf("%s:%d records that the port %s the original's fix, and the original's fix there "+
				"type-checks", each.file, each.line, each.kind)
		}
	}
}

// fixedErrors is every type error of the differences fixture with the fixes of found applied, each file's edits
// from the last to the first, loaded through an overlay of the fixed files.
func fixedErrors(test *testing.T, found []diagnostic) []string {
	test.Helper()
	directory := analysistest.TestData()
	overlay := map[string][]byte{}
	for _, pattern := range differencesFixtures {
		names, globError := filepath.Glob(filepath.Join(directory, "src", filepath.FromSlash(pattern), "*.go"))
		if globError != nil {
			test.Fatalf("listing %s: %v", pattern, globError)
		}
		for _, name := range names {
			if _, isOverlaid := overlay[name]; isOverlaid {
				continue
			}
			// Findings name their file by its base name, so the fixture's files are named apart across its packages.
			for other := range overlay {
				if filepath.Base(other) == filepath.Base(name) {
					test.Fatalf("%s and %s share a base name, which the findings cannot tell apart", other, name)
				}
			}
			source, readError := os.ReadFile(name)
			if readError != nil {
				test.Fatal(readError)
			}
			overlay[name] = applied(test, filepath.Base(name), source, editsIn(found, filepath.Base(name)))
		}
	}
	loaded, loadError := load.PackagesWithOverlay(directory, differencesFixtures, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil, overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %v: %v", differencesFixtures, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %v found no package, so the comparison would prove nothing", differencesFixtures)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		for _, each := range root.Errors {
			errors = append(errors, each.Error())
		}
	}
	return errors
}

// editsIn is every edit of every fix of found in the file named file.
func editsIn(found []diagnostic, file string) []offsetEdit {
	edits := []offsetEdit{}
	for _, each := range found {
		// Every fix of S1002 replaces the comparison its finding is at, so a finding's file is its edits'.
		if each.file != file {
			continue
		}
		for _, suggested := range each.fixes {
			edits = append(edits, suggested.edits...)
		}
	}
	return edits
}

// applied is source with edits applied, from the last to the first, so each edit's offsets still name the bytes they
// were read against. Two edits that overlap fail the test.
func applied(test *testing.T, file string, source []byte, edits []offsetEdit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(first offsetEdit, second offsetEdit) int {
		return cmp.Compare(second.start, first.start)
	})
	fixed := slices.Clone(source)
	for index, each := range ordered {
		if index > 0 && each.end > ordered[index-1].start {
			test.Fatalf("the edits to %s overlap at offset %d", file, each.start)
		}
		fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
	}
	return fixed
}

// recordPattern is one record of the differences fixture, the text after its `// port ` opening: `moves:` and the
// message of a finding both make, whose fix the port writes otherwise.
var recordPattern = regexp.MustCompile(`^(moves): (.+?)\s*$`)

// record is one record of the differences fixture: the file and line it is on, its kind and the message it names.
type record struct {
	file    string
	line    int
	kind    string
	message string
}

// recordsOf reads every `// port ` comment of the .go files in the fixture packages patterns name, as the parity
// harness reads its records: as the parser finds comments, so a `// port ` inside a string is no record, and a comment
// it cannot read fails, and so does a record with no comment on the line above it, which says why.
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
			source, readError := os.ReadFile(name)
			if readError != nil {
				test.Fatal(readError)
			}
			lines := strings.Split(string(source), "\n")
			file, parseError := parser.ParseFile(
				fileSet, name, source, parser.ParseComments|parser.SkipObjectResolution,
			)
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
						if line < 2 || !strings.HasPrefix(strings.TrimSpace(lines[line-2]), "//") {
							test.Fatalf("%s:%d records %q with no reason on the line above", filepath.Base(name), line,
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

// placeOf is where found starts and what it says, which a moved finding keeps.
func placeOf(found diagnostic) string {
	return fmt.Sprintf("%s:%d:%d: %s", found.file, found.line, found.column, found.message)
}

// described is found as one line, with where it ends and every fix and edit it suggests.
func described(found diagnostic) string {
	return describedWith(found, true)
}

// describedWithoutText is found described with every edit's range and none of its new text, the part of a moved
// finding that stays the original's.
func describedWithoutText(found diagnostic) string {
	return describedWith(found, false)
}

// describedWith is found as one line, with where it ends and every fix and edit it suggests, each edit's new text
// included when isTextIncluded.
func describedWith(found diagnostic, isTextIncluded bool) string {
	text := &strings.Builder{}
	fmt.Fprintf(text, "%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
	for _, each := range found.fixes {
		fmt.Fprintf(text, " fix %q", each.message)
		for _, edit := range each.edits {
			fmt.Fprintf(text, " [%d,%d)", edit.start, edit.end)
			if isTextIncluded {
				fmt.Fprintf(text, " %q", edit.newText)
			}
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

// diagnosticsOf loads the fixture packages patterns name from the tree at directory, runs staticcheck's S1002 and
// the port over them in one checker, and returns each one's findings with their ends and fixes, sorted.
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
	// The checker gets one variant of each package, as staticcheck's runner analyzes, since Roots holds a package with
	// in-package tests twice, alone and with them, and would report its findings, and suggest its edits, twice.
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{s1002.Analyzer, Analyzer}, loaded.Packages, &checker.Options{},
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
			case s1002.Analyzer:
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
