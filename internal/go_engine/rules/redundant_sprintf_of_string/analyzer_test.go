package redundant_sprintf_of_string

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
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1025"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1025, which it keeps four times, under simple/s1025/testdata/go1.0,
// go1.9, go1.17 and go1.18, each with its golden file, each copied unchanged into the module of its folder's name.
// S1025 reads the language version where it judges a slice of a type defined on byte, which converts to a string
// from go1.18 on, so the version matters: staticcheck's testutil loads each folder as a module whose go.mod, overlaid,
// says `go 1.N` for its folder's name, and the go line is what its loader and ours alike hand the type checker as the
// package's version, which code.LanguageVersion reads. So each folder is a module, example.com as staticcheck's is,
// whose go.mod says its folder's version, and every load checks that the version reached the package.
const upstream = "CheckRedundantSprintf"

// upstreamFile is the upstream fixture's one file under every version, beside its golden file.
const upstreamFile = "LintRedundantSprintf.go"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's four.
var versions = []string{"go1.0", "go1.9", "go1.17", "go1.18"}

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched, as the parity
// harness loads a fixture directory that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// agreeingFixtures is every fixture package of each module where the original and the port agree, fixes and all:
// staticcheck's in each, a file tagged past its go1.17 module's version, and in go1.18 parity's with its dot import,
// its package named fmt that is not fmt, and the package that calls it.
var agreeingFixtures = map[string][]string{
	"go1.0":  {"./" + upstream},
	"go1.9":  {"./" + upstream},
	"go1.17": {"./" + upstream, "./tagged"},
	"go1.18": {"./" + upstream, "./parity", "./parity/dotted", "./parity/impostor", "./parity/impostor/fmt"},
}

// differencesVersion is the module the differences fixture is in.
const differencesVersion = "go1.18"

// differencesFixtures is the differences fixture's packages: its own, one that declares a string of its own at
// package scope, and one that holds the finding the port drops.
var differencesFixtures = []string{"./differences", "./differences/universe", "./differences/dropped"}

// sharedDifferencesFixtures is the differences fixture's packages whose findings the two share, whose fixes alone
// part, so the parity harness, which compares findings without their fixes, holds them identical.
var sharedDifferencesFixtures = []string{"./differences", "./differences/universe"}

// regroupedFile is the differences fixture's file whose one move the original's fix compiles at, regrouping the
// argument so it means another value, which no type error shows and the fixed line's text does.
const regroupedFile = "regrouped.go"

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.17 fixture holds no note, so there
// the original must report nothing too, as staticcheck's own test requires of it, and the call it holds is the one
// the go1.18 fixture's note expects, so the version is what parts them.
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
		original, port := parity.Findings(test, s1025.Analyzer, Analyzer, module(version), "./"+upstream)
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

// The port's fixes, applied to the upstream fixture of each version, make its golden file, as staticcheck's testutil
// requires of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when
// it holds no sections, as none of the four does, and the fixed file is gofmt'd before it is compared. The go1.17
// golden file is its fixture unchanged, which no fix touches.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	edited := 0
	for _, version := range versions {
		_, port := diagnosticsOf(test, module(version), version, "./"+upstream)
		path := filepath.Join(module(version), upstream, upstreamFile)
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
			test.Fatalf("the %s golden file holds %d sections, one a fix's message, which this test does not read",
				version, len(archive.Files))
		}
		edits := editsIn(port, upstreamFile)
		edited += len(edits)
		test.Logf("under %s the port found %d, with %d edits", version, len(port), len(edits))
		fixed := applied(test, upstreamFile, source, edits)
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes under %s make source gofmt cannot format: %v\n%s", version, formatError,
				fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes under %s make\n%s\nand the golden file is\n%s", version, formatted,
				archive.Comment)
		}
	}
	if edited == 0 {
		test.Fatal("the port made no edit to the upstream fixtures, so the comparison proves nothing")
	}
}

// The original and the port run in one checker over every module's fixtures where they agree, parity's holding every
// shape the query and the judgments branch on and the near miss of each, a generated file, a renamed and a dot
// import, a package named fmt that is not fmt, and the places each fix stands, and over the differences fixture's
// packages whose findings the two share and whose fixes alone part, and their findings must be the same multiset of
// file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		patterns := agreeingFixtures[version]
		if version == differencesVersion {
			patterns = slices.Concat(patterns, sharedDifferencesFixtures)
		}
		parity.RequireIdentical(test, s1025.Analyzer, Analyzer, module(version), patterns...)
	}
}

// The pattern looks through doubled parentheses around the callee, the format and the argument, and the parser keeps
// a stray semicolon, shapes the house's testdata cannot hold, since its guard holds testdata to gofmt, so the fixture
// is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1025.Analyzer, Analyzer, unformattedTree(test), "unformatted")
}

// Over every fixture where the two agree, each finding's end and every suggested fix, its message and each edit's
// start, end and new text, are the original's, which the parity harness does not compare yet. Both build each fix
// through edit.ReplaceWithNode from the argument the query binds, so this is where the binding is held to the
// original's, its parentheses removed, and every replacement byte for byte, the gofmt-rewritable fixture's included.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	compared := 0
	fixes := 0
	for _, version := range versions {
		original, port := diagnosticsOf(test, module(version), version, agreeingFixtures[version]...)
		compared += requireSameDiagnostics(test, version, original, port)
		for _, each := range original {
			fixes += len(each.fixes)
		}
	}
	original, port := diagnosticsOf(test, unformattedTree(test), "", "unformatted")
	compared += requireSameDiagnostics(test, "unformatted", original, port)
	test.Logf("the two agree on %d findings, with %d fixes", compared, fixes)
	if fixes == 0 {
		test.Fatal("the original suggested no fix in the fixtures, so the comparison proves nothing")
	}
}

// requireSameDiagnostics fails the test for every finding of original the port does not make to the byte, and every
// one the port makes the original does not, and is how many the original made, which must be some.
func requireSameDiagnostics(test *testing.T, name string, original []diagnostic, port []diagnostic) int {
	test.Helper()
	originalDescribed := describedAll(original)
	portDescribed := describedAll(port)
	test.Logf("under %s the original found %d, the port %d", name, len(originalDescribed), len(portDescribed))
	if len(originalDescribed) == 0 {
		test.Fatalf("under %s the original found nothing, so the comparison proves nothing", name)
	}
	for _, missing := range difference(originalDescribed, portDescribed) {
		test.Errorf("under %s the original found %s, and the port did not", name, missing)
	}
	for _, extra := range difference(portDescribed, originalDescribed) {
		test.Errorf("under %s the port found %s, and the original did not", name, extra)
	}
	return len(originalDescribed)
}

// Over the differences fixture, the port's findings, with their ends and fixes, are the original's with each record
// applied and nothing else: a finding a `drops:` record names is one the port does not make, a fix the original
// suggests where a `withholds:` record names it is one the port's same finding leaves out, and a finding where a
// `moves:` record names it is the original's at the same range with the same fixes, each edit over the same bytes,
// and every edit's new text other than the original's. Every record must name exactly one of the original's findings
// or fixes, and every shape no record is on must be the original's to the byte. The parity harness reads `finds:` and
// `renames:` records alone so far (#dcgn32d), so this test holds the port's three kinds, and the port finds and
// renames nothing.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, module(differencesVersion), differencesVersion, differencesFixtures...)
	isMovedAt := map[string]bool{}
	expectedDescribed := []string{}
	taken := make([]int, len(records))
	kinds := map[string]int{}
	for _, found := range original {
		isMoved := false
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
		if isDropped {
			continue
		}
		if isMoved {
			isMovedAt[placeOf(found)] = true
			expectedDescribed = append(expectedDescribed, describedWithoutText(found))
			continue
		}
		expectedDescribed = append(expectedDescribed, described(found))
	}
	for _, each := range records {
		kinds[each.kind]++
	}
	test.Logf("the original found %d, the port %d, the fixture records %d: %d drops, %d moves, %d withholds",
		len(original), len(port), len(records), kinds["drops"], kinds["moves"], kinds["withholds"])
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

// Applied to the differences fixture, the port's fixes make packages that type-check, and the original's make ones
// that do not, on the line of every record that moves or withholds a fix: the original leaves a bare value as a
// statement, regroups an operator under an index, lands a composite literal where the parser reads a block, or calls
// a string that is no type, and the port writes what compiles or nothing. A drop is of a finding whose fix compiles,
// which no fix applied could tell, and so is the move in regroupedFile, whose meaning
// TestPortMovedFixesKeepTheirMeaning holds instead. The fixed files are loaded through an overlay over the fixtures on
// disk, which changes no import, so go list's answer for the module holds.
func TestPortMovedFixesCompile(test *testing.T) {
	test.Parallel()
	records := recordsOf(test, differencesFixtures)
	original, port := diagnosticsOf(test, module(differencesVersion), differencesVersion, differencesFixtures...)
	portErrors := fixedErrors(test, port)
	for _, each := range portErrors {
		test.Errorf("the port's fixes make code that does not type-check: %s", each)
	}
	originalErrors := fixedErrors(test, original)
	test.Logf("the port's fixed fixture has %d errors, the original's %d", len(portErrors), len(originalErrors))
	for _, each := range records {
		if each.kind == "drops" || each.file == regroupedFile {
			continue
		}
		at := string(filepath.Separator) + each.file + ":" + strconv.Itoa(each.line) + ":"
		isBroken := slices.ContainsFunc(originalErrors, func(message string) bool {
			return strings.Contains(message, at)
		})
		if !isBroken {
			test.Errorf("%s:%d records that the port %s the original's fix, and the original's fixed line has no "+
				"error", each.file, each.line, each.kind)
		}
	}
}

// The move in regroupedFile is held by the text it writes: applied, the port's fix slices the whole argument,
// `(first + second)[1:]`, and the original's slices second alone, `first + second[1:]`, which compiles, so a port
// that left the parentheses out would pass every other test and change what the code means.
func TestPortMovedFixesKeepTheirMeaning(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, module(differencesVersion), differencesVersion, differencesFixtures...)
	records := recordsOf(test, differencesFixtures)
	line := 0
	for _, each := range records {
		if each.file == regroupedFile {
			if each.kind != "moves" || line != 0 {
				test.Fatalf("%s holds a %s record at line %d, where it is to hold one move alone", regroupedFile,
					each.kind, each.line)
			}
			line = each.line
		}
	}
	if line == 0 {
		test.Fatalf("%s records no move, so the comparison proves nothing", regroupedFile)
	}
	source, readError := os.ReadFile(filepath.Join(module(differencesVersion), "differences", regroupedFile))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, side := range []struct {
		name     string
		found    []diagnostic
		expected string
	}{
		{name: "port", found: port, expected: "return (first + second)[1:]"},
		{name: "original", found: original, expected: "return first + second[1:]"},
	} {
		edits := editsIn(side.found, regroupedFile)
		if len(edits) != 1 {
			test.Fatalf("the %s makes %d edits to %s, where the move is one", side.name, len(edits), regroupedFile)
		}
		fixed := strings.Split(string(applied(test, regroupedFile, source, edits)), "\n")
		// Each edit replaces the call within its one line, so the fixed line is where the record is.
		code, _, _ := strings.Cut(fixed[line-1], " //")
		test.Logf("the %s's fixed line is %q", side.name, strings.TrimSpace(code))
		if strings.TrimSpace(code) != side.expected {
			test.Errorf("the %s's fix makes %q of %s:%d, where %q was expected", side.name, strings.TrimSpace(code),
				regroupedFile, line, side.expected)
		}
	}
}

// fixedErrors is every error of the differences fixture with the fixes of found applied, each file's edits from the
// last to the first, loaded through an overlay of the fixed files.
func fixedErrors(test *testing.T, found []diagnostic) []string {
	test.Helper()
	directory := module(differencesVersion)
	overlay := map[string][]byte{}
	for _, pattern := range differencesFixtures {
		names, globError := filepath.Glob(filepath.Join(directory, filepath.FromSlash(pattern), "*.go"))
		if globError != nil {
			test.Fatalf("listing %s: %v", pattern, globError)
		}
		for _, name := range names {
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
		// Every fix of S1025 replaces the call its finding is at, so a finding's file is its edits'.
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

// recordPattern is one record of the differences fixture, the text after its `// port ` opening: `drops:` and the
// message of a finding the original makes and the port does not, `withholds:` and the message of a fix the original
// suggests and the port, making the same finding, leaves out, or `moves:` and the message of a finding both make,
// whose fix the port writes otherwise.
var recordPattern = regexp.MustCompile(`^(drops|withholds|moves): (.+?)\s*$`)

// record is one record of the differences fixture: the file and line it is on, its kind and the message it names.
type record struct {
	file    string
	line    int
	kind    string
	message string
}

// recordsOf reads every `// port ` comment of the .go files in the differences module's packages patterns name, as
// the parity harness reads its records: as the parser finds comments, so a `// port ` inside a string is no record,
// and a comment it cannot read fails, and so does a record with no comment on the line above it, which says why.
func recordsOf(test *testing.T, patterns []string) []record {
	test.Helper()
	records := []record{}
	fileSet := token.NewFileSet()
	for _, pattern := range patterns {
		directory := filepath.Join(module(differencesVersion), filepath.FromSlash(pattern))
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
// test's own, and returns the tree. It loads with no module, so its version is none, which the go1.18 judgment of a
// slice of bytes reads as older on both sides.
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

// diagnosticsOf loads the packages patterns name from the fixture directory, a module of version or, where version is
// empty, a tree laid out by import path, runs staticcheck's S1025 and the port over them in one checker, as
// parity.Findings does, and returns each one's findings with their ends and fixes, sorted. Every package a module
// loads must carry version as its own, the go line its go.mod says, which both analyzers' judgment of a slice of a
// type defined on byte reads.
func diagnosticsOf(
	test *testing.T, directory string, version string, patterns ...string,
) (original []diagnostic, port []diagnostic) {
	test.Helper()
	environment := moduleEnvironment
	if version == "" {
		environment = []string{"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off"}
	}
	loaded, loadError := load.Packages(directory, patterns, environment, nil)
	if loadError != nil {
		test.Fatalf("loading %v from %s: %v", patterns, directory, loadError)
	}
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v from %s found no package, so the comparison would prove nothing", patterns, directory)
	}
	for _, root := range loaded.Roots {
		// A fixture that does not type-check is one both sides skip, and would compare nothing.
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean: %v", root.ID, root.Errors)
		}
		if version != "" && root.Types.GoVersion() != version {
			test.Fatalf("fixture package %s loaded under %q, where its module says %s", root.ID,
				root.Types.GoVersion(), version)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{s1025.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing %v from %s: %v", patterns, directory, analyzeError)
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
			case s1025.Analyzer:
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
