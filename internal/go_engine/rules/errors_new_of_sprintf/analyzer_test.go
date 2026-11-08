package errors_new_of_sprintf

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
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
	"honnef.co/go/tools/simple/s1028"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1028, copied unchanged from
// simple/s1028/testdata/go1.0/CheckErrorsNewSprintf into testdata/src/CheckErrorsNewSprintf with its golden file. The
// go1.0 folder sets the language version of staticcheck's run, which S1028 never reads, gating nothing, so the
// fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckErrorsNewSprintf"

// upstreamFile is the upstream fixture's one file, beside its golden file.
const upstreamFile = "LintErrorsNewSprintf.go"

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's, parity's, parity's dot and renamed imports beside fmt's own name, and the package at fmt's external
// tests' path, which imports fmt as any other package does.
var fixtures = []string{upstream, "parity", "parity/dotted", "fmt_test"}

// differencesFixtures is the differences fixture's packages: its own, which imports fmt under another name alone, one
// that imports fmt into its own scope alone, one where a local takes the name fmt, one that names another package
// fmt, and one that imports fmt under its own name and spreads a list into Sprintf.
var differencesFixtures = []string{
	"differences", "differences/dotted", "differences/shadowed", "differences/spelled", "differences/spread",
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
	original, port := parity.Findings(test, s1028.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
		})
	}
	test.Logf("the fixture expects %d, the original found %d, the port %d", len(wants), len(original), len(found))
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
// sections, and the fixed file is gofmt'd before it is compared.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := diagnosticsOf(test, analysistest.TestData(), upstream)
	path := filepath.Join(analysistest.TestData(), "src", upstream, upstreamFile)
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
	edits := editsIn(port, upstreamFile)
	test.Logf("the port found %d, with %d edits", len(port), len(edits))
	if len(edits) == 0 {
		test.Fatal("the port made no edit to the upstream fixture, so the comparison proves nothing")
	}
	fixed := applied(test, upstreamFile, source, edits)
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
	}
	if !bytes.Equal(formatted, archive.Comment) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", formatted, archive.Comment)
	}
}

// The original and the port run in one checker over staticcheck's fixture, parity's, which holds every shape the
// query branches on and the near miss of each, a generated file, and dot and renamed imports, the package at fmt's
// external tests' path, and the differences fixture, whose findings the two share and whose fixes alone part, and
// their findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1028.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(fixtures, differencesFixtures)...)
}

// Over every fixture where the two agree, each finding's end and every suggested fix, its message and each edit's
// start, end and new text, are the original's, which the parity harness does not compare yet. The original builds its
// fix through code.EditMatch from the arguments its query binds, and the port through edit.ReplaceWithNode from the
// ones it binds, so this is where the two are held byte for byte, dot and renamed imports beside fmt's own name,
// arguments across lines and comments among them included.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, analysistest.TestData(), fixtures...)
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

// Over the differences fixture, the port's findings, with their ends and fixes, are the original's with each record
// applied and nothing else: a fix the original suggests where a `withholds:` record names it is one the port's same
// finding leaves out, and a finding where a `moves:` record names it is the original's at the same range with the
// same fixes, each edit over the same bytes, and every edit's new text other than the original's, which
// TestPortMovedFixesMakeGoldens holds by its text. Every record must name exactly one of the original's findings or
// fixes, and every shape no record is on must be the original's to the byte. The parity harness reads `finds:` and
// `renames:` records alone so far (#dcgn32d), so this test holds the port's two kinds, and the port finds, drops and
// renames nothing.
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
			if each.file != found.file || each.line != found.line {
				continue
			}
			switch each.kind {
			case "moves":
				if each.message == found.message {
					isMoved = true
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

// The port's fixes, applied to each differences file a `moves:` record is in, make the file's golden file beside it,
// as gofmt prints it, so a move's edits are held by their text: the name the code calls Sprintf by in place of fmt,
// Errorf alone under a dot import, and a spread's ellipsis kept. Every golden file belongs to a file a move is in,
// and every such file has one.
func TestPortMovedFixesMakeGoldens(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	_, port := diagnosticsOf(test, analysistest.TestData(), differencesFixtures...)
	moved := []string{}
	for _, each := range records {
		if each.kind == "moves" && !slices.Contains(moved, each.file) {
			moved = append(moved, each.file)
		}
	}
	if len(moved) == 0 {
		test.Fatal("the differences fixture records no move, so the comparison proves nothing")
	}
	// The differences fixture's packages each sit in a directory of their own, and fixedErrors holds their files'
	// base names apart, so a moved file is found by its base name in whichever directory holds it.
	compared := 0
	for _, pattern := range differencesFixtures {
		directory := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern))
		goldens, globError := filepath.Glob(filepath.Join(directory, "*.golden"))
		if globError != nil {
			test.Fatalf("listing %s: %v", directory, globError)
		}
		for _, golden := range goldens {
			if !slices.Contains(moved, strings.TrimSuffix(filepath.Base(golden), ".golden")) {
				test.Errorf("%s is the golden file of a file no move is in", filepath.Base(golden))
			}
		}
		for _, file := range moved {
			path := filepath.Join(directory, file)
			source, readError := os.ReadFile(path)
			if os.IsNotExist(readError) {
				continue
			}
			if readError != nil {
				test.Fatal(readError)
			}
			golden, goldenError := os.ReadFile(path + ".golden")
			if goldenError != nil {
				test.Fatal(goldenError)
			}
			edits := editsIn(port, file)
			test.Logf("%s: the port made %d edits", file, len(edits))
			formatted, formatError := format.Source(applied(test, file, source, edits))
			if formatError != nil {
				test.Fatalf("the port's fixes to %s make source gofmt cannot format: %v", file, formatError)
			}
			if !bytes.Equal(formatted, golden) {
				test.Errorf("the port's fixes to %s make\n%s\nand its golden file is\n%s", file, formatted, golden)
			}
			compared++
		}
	}
	if compared != len(moved) {
		test.Errorf("%d files hold a move, and %d were compared with a golden file", len(moved), compared)
	}
}

// Applied to the differences fixture, the port's fixes make packages that type-check, and every record is on a
// finding the original's fix gets wrong: either its fixes make the record's file fail to type-check, where the
// original names fmt.Errorf by a name the code does not give it and the port by the one it does, or, where none does,
// not at all; or the record is a move whose original fix drops the ellipsis of a spread into Sprintf, which still
// type-checks and hands Errorf the list as one value, and whose port fix keeps it. The fixed files are loaded through
// an overlay over the fixtures on disk, which changes no import, so go list's answer for the tree holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, analysistest.TestData(), differencesFixtures...)
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	spreadsKept := 0
	for _, each := range records {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+each.file+":")
		})
		isSpreadKept := each.kind == "moves" && keepsDroppedSpread(test, original, port, each)
		if isSpreadKept {
			spreadsKept++
		}
		if !isBroken && !isSpreadKept {
			test.Errorf("%s:%d records that the port %s the original's fix, and the original's fixes to %s "+
				"type-check and drop no spread the port's keep", each.file, each.line, each.kind, each.file)
		}
	}
	test.Logf("the port's fixed fixture has %d errors, the original's %d, and the port keeps %d spreads the "+
		"original drops", len(portErrors), len(originalErrors), spreadsKept)
}

// keepsDroppedSpread is whether, at the finding a record is on, the original's one fix writes a call with no
// ellipsis and the port's writes it with one.
func keepsDroppedSpread(test *testing.T, original []diagnostic, port []diagnostic, each record) bool {
	test.Helper()
	originalCall := fixedCall(test, original, each)
	portCall := fixedCall(test, port, each)
	return originalCall != nil && portCall != nil && !originalCall.Ellipsis.IsValid() && portCall.Ellipsis.IsValid()
}

// fixedCall is the call the one edit of the one fix of the finding on a record's line writes, parsed, or nil where
// no finding there has one.
func fixedCall(test *testing.T, found []diagnostic, each record) *ast.CallExpr {
	test.Helper()
	for _, candidate := range found {
		if candidate.file != each.file || candidate.line != each.line || len(candidate.fixes) != 1 ||
			len(candidate.fixes[0].edits) != 1 {
			continue
		}
		expression, parseError := parser.ParseExpr(candidate.fixes[0].edits[0].newText)
		if parseError != nil {
			test.Fatalf("%s:%d: the fix writes %q, which is no expression: %v", each.file, each.line,
				candidate.fixes[0].edits[0].newText, parseError)
		}
		call, isCall := expression.(*ast.CallExpr)
		if !isCall {
			test.Fatalf("%s:%d: the fix writes %q, which is no call", each.file, each.line,
				candidate.fixes[0].edits[0].newText)
		}
		return call
	}
	return nil
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
		// Every fix of S1028 replaces the call its finding is at, so a finding's file is its edits'.
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

// recordPattern is one record of the differences fixture, the text after its `// port ` opening: `withholds:` and
// the message of a fix the original suggests and the port, making the same finding, leaves out, or `moves:` and the
// message of a finding both make, whose fix the port writes otherwise.
var recordPattern = regexp.MustCompile(`^(withholds|moves): (.+?)\s*$`)

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

// diagnosticsOf loads the fixture packages patterns name from the tree at directory, runs staticcheck's S1028 and
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
		[]*analysis.Analyzer{s1028.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1028.Analyzer:
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
