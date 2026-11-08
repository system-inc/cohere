package time_until

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
	"honnef.co/go/tools/simple/s1024"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1024, which it keeps twice, under simple/s1024/testdata/go1.7 and
// go1.8, the second with its golden file, each copied unchanged into the module of its folder's name. S1024 reports
// only where the standard library is go1.8 or newer, so the version matters: staticcheck's testutil loads each folder
// as a module whose go.mod, overlaid, says `go 1.N` for its folder's name, and the go line is what its loader and
// ours alike hand the type checker as the package's version, which the gate reads. So testdata/src/go1.7 and go1.8
// are each a module, example.com as staticcheck's is, whose go.mod says its folder's version, and every load checks
// that the version reached the package, so the go1.7 fixture's silence is the gate's and not a version lost.
const upstream = "CheckTimeUntil"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's two.
var versions = []string{"go1.7", "go1.8"}

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched, as the parity
// harness loads a fixture directory that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// upstreamFile is the upstream fixture's one file, beside its golden file under go1.8.
const upstreamFile = "LimeTimeUntil.go"

// fixtures is every fixture package of the go1.8 module where the original and the port agree, fixes and all:
// staticcheck's, parity's, and parity's that imports time into its own scope beside its own name.
var fixtures = []string{"./" + upstream, "./parity", "./parity/dotted"}

// differencesFixtures is the differences fixture's packages, where the two make the same findings and the original's
// fixes fail to compile: its own, which imports time as clock alone in one file and hands Sub receivers that are no
// time.Time in the other, and one that imports time into its own scope alone. Its embedder package, which it imports,
// holds no call.
var differencesFixtures = []string{"./differences", "./differences/dotted"}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.7 fixture holds no note, so there
// the original must report nothing too, as staticcheck's own test requires of it, and the call it holds is the one
// the go1.8 fixture's note expects, so the gate is what parts them.
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
		diagnosticsOf(test, version, "./"+upstream)
		original, port := parity.Findings(test, s1024.Analyzer, Analyzer, module(version), "./"+upstream)
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

// The port's fixes, applied to the go1.8 upstream fixture, make its golden file, as staticcheck's testutil requires
// of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when it holds
// no sections, and the fixed file is gofmt'd before it is compared. The go1.7 fixture has no golden file, and no fix.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := diagnosticsOf(test, "go1.8", "./"+upstream)
	path := filepath.Join(module("go1.8"), upstream, upstreamFile)
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

// The original and the port run in one checker over staticcheck's go1.8 fixture, parity's, which holds every shape the
// query branches on and the near miss of each, a generated file, files tagged older and newer than the module, and a
// renamed and a dot import beside time's own name, and the differences fixture, whose findings the two share and whose
// fixes alone part, and their findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1024.Analyzer, Analyzer, module("go1.8"),
		slices.Concat(fixtures, differencesFixtures)...)
}

// Over every fixture where the two agree, under both versions, each finding's end and every suggested fix, its message
// and each edit's start, end and new text, are the original's, which the parity harness does not compare yet. The
// fix's text is the one the original builds through pattern.NodeToAST and the port itself, so this is where the two
// are held byte for byte, a renamed and a dot import beside time's own name, a parenthesized receiver and receivers
// across lines among them.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "go1.8", fixtures...)
	olderOriginal, olderPort := diagnosticsOf(test, "go1.7", "./...")
	originalDescribed := describedAll(slices.Concat(original, olderOriginal))
	portDescribed := describedAll(slices.Concat(port, olderPort))
	test.Logf("the original found %d, the port %d", len(originalDescribed), len(portDescribed))
	if len(originalDescribed) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	fixes := 0
	for _, each := range original {
		fixes += len(each.fixes)
	}
	if fixes == 0 || fixes == len(original) {
		test.Fatalf("of the original's %d findings %d carry a fix, so the fixture does not hold both branches",
			len(original), fixes)
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
// same fixes, each edit over the same bytes, and every edit's new text other than the original's, a text
// TestPortMovedFixesMakeGoldens holds. Every record must
// name exactly one of the original's findings or fixes, and every shape no record is on must be the original's to the
// byte. The parity harness reads `finds:` and `renames:` records alone so far (#dcgn32d), so this test holds the
// port's two kinds, and the port finds, drops and renames nothing.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, "go1.8", differencesFixtures...)
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

// The port's fixes, applied to each differences file a `moves:` record is in, make the file's golden file, as gofmt
// prints it, so a move's edits are held by their text, which the records test compares by range alone: the name the
// code gives time, the pointer dereferenced and the embedded field selected, by its name alone or by the path, which
// a wrong field that still compiles would fail here and nowhere else. Every golden file belongs to a file a move is
// in, and every file a move is in has one.
func TestPortMovedFixesMakeGoldens(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	_, port := diagnosticsOf(test, "go1.8", differencesFixtures...)
	moved := []string{}
	for _, each := range records {
		if each.kind == "moves" && !slices.Contains(moved, each.file) {
			moved = append(moved, each.file)
		}
	}
	if len(moved) == 0 {
		test.Fatal("the differences fixture records no move, so the comparison proves nothing")
	}
	compared := []string{}
	for _, pattern := range differencesFixtures {
		directory := filepath.Join(module("go1.8"), filepath.FromSlash(pattern))
		goldens, globError := filepath.Glob(filepath.Join(directory, "*.golden"))
		if globError != nil {
			test.Fatalf("listing %s: %v", directory, globError)
		}
		for _, golden := range goldens {
			if !slices.Contains(moved, strings.TrimSuffix(filepath.Base(golden), ".golden")) {
				test.Errorf("%s is the golden file of a file no move is in", filepath.Base(golden))
			}
		}
		// The records name a file by its base name, and the fixture's packages each hold their own files.
		names, namesError := filepath.Glob(filepath.Join(directory, "*.go"))
		if namesError != nil {
			test.Fatalf("listing %s: %v", directory, namesError)
		}
		for _, path := range names {
			file := filepath.Base(path)
			if !slices.Contains(moved, file) {
				continue
			}
			compared = append(compared, file)
			source, readError := os.ReadFile(path)
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
		}
	}
	for _, file := range moved {
		if !slices.Contains(compared, file) {
			test.Errorf("%s holds a move, and no fixture directory holds the file", file)
		}
	}
}

// Applied to the differences fixture, the port's fixes make packages that type-check, and the original's make ones
// that do not, in every file a record is in: the original names time.Until by a name the file does not give it, or
// hands it a receiver that is no time.Time, and the port names both as the code does, or, where nothing does, writes
// no fix. The fixed files are loaded through an overlay over the fixtures on disk, which changes no import, so go
// list's answer for the module holds, and the load checks the version as every load here does.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, "go1.8", differencesFixtures...)
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	for _, each := range records {
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, string(filepath.Separator)+each.file+":")
		})
		if !isBroken {
			test.Errorf("%s:%d records that the port %s the original's fix, and the original's fixes to %s "+
				"type-check", each.file, each.line, each.kind, each.file)
		}
	}
}

// fixedErrors is every type error of the differences fixture with the fixes of found applied, each file's edits
// from the last to the first, loaded through an overlay of the fixed files.
func fixedErrors(test *testing.T, found []diagnostic) []string {
	test.Helper()
	directory := module("go1.8")
	overlay := map[string][]byte{}
	for _, pattern := range differencesFixtures {
		names, globError := filepath.Glob(filepath.Join(directory, filepath.FromSlash(pattern), "*.go"))
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
	loaded, loadError := load.PackagesWithOverlay(directory, differencesFixtures, moduleEnvironment, nil, overlay)
	if loadError != nil {
		test.Fatalf("loading the fixed %v: %v", differencesFixtures, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading the fixed %v found no package, so the comparison would prove nothing", differencesFixtures)
	}
	errors := []string{}
	for _, root := range loaded.Roots {
		if root.Types.GoVersion() != "go1.8" {
			test.Fatalf("fixed package %s loaded under %q, where its module says go1.8", root.ID,
				root.Types.GoVersion())
		}
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
		// Every fix of S1024 replaces the call its finding is at, so a finding's file is its edits'.
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

// recordsOf reads every `// port ` comment of the .go files in the go1.8 module's fixture packages patterns name, as
// the parity harness reads its records: as the parser finds comments, so a `// port ` inside a string is no record,
// and a comment it cannot read fails, and so does a record with no comment on the line above it, which says why.
func recordsOf(test *testing.T, patterns []string) []record {
	test.Helper()
	records := []record{}
	fileSet := token.NewFileSet()
	for _, pattern := range patterns {
		directory := filepath.Join(module("go1.8"), filepath.FromSlash(pattern))
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

// diagnosticsOf loads the packages patterns name from the fixture module of version, runs staticcheck's S1024 and the
// port over them in one checker, as parity.Findings does, and returns each one's findings with their ends and fixes,
// sorted. Every package loaded must carry version as its own, the go line its module's go.mod says, which is what
// both analyzers' version gates read.
func diagnosticsOf(test *testing.T, version string, patterns ...string) (original []diagnostic, port []diagnostic) {
	test.Helper()
	loaded, loadError := load.Packages(module(version), patterns, moduleEnvironment, nil)
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
		[]*analysis.Analyzer{s1024.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1024.Analyzer:
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
