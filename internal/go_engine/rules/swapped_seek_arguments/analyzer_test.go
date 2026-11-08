package swapped_seek_arguments

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/staticcheck/sa1013"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamFixtures are staticcheck's own fixtures for SA1013, copied unchanged from
// staticcheck/sa1013/testdata/go1.0/checkStdlibUsageSeeker into testdata/src/checkStdlibUsageSeeker with its golden
// file. The go1.0 folder sets the language version of staticcheck's run, which SA1013 never reads, so the fixture loads
// from a tree laid out by import path as the house's fixtures do.
var upstreamFixtures = []string{"checkStdlibUsageSeeker"}

// Every `//@ diag(...)` note the upstream fixtures hold, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	wants := []staticcheck_wants.Want{}
	found := []staticcheck_wants.Finding{}
	for _, pattern := range upstreamFixtures {
		fixture := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern))
		fixtureWants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		wants = append(wants, fixtureWants...)
		_, port := diagnosticsOf(test, pattern)
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.file), Line: each.line, Message: each.message,
			})
		}
	}
	if len(wants) == 0 {
		test.Fatal("the upstream fixtures hold no notes, so the comparison proves nothing")
	}
	test.Logf("the fixtures expect %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// The port's fixes, applied to each upstream file they edit, make the file's golden, as staticcheck's testutil
// requires of the original's (analysis/lint/testutil/check.go): the golden is read as a txtar archive; with no
// sections its comment is the file with every fix applied, and with sections each is named for a fix's message and
// holds the file with that message's fixes applied, every message the port suggests having its section and every
// section a message. The fixed file is gofmt'd before it is compared, and a section's trailing blank lines are
// trimmed to one newline, as testutil does.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	compared := 0
	for _, pattern := range upstreamFixtures {
		_, port := diagnosticsOf(test, pattern)
		// Each file's edits, by the message of the fix they belong to.
		edits := map[string]map[string][]offsetEdit{}
		for _, diagnostic := range port {
			for _, fix := range diagnostic.fixes {
				for _, edit := range fix.edits {
					path := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern), edit.file)
					if edits[path] == nil {
						edits[path] = map[string][]offsetEdit{}
					}
					edits[path][fix.message] = append(edits[path][fix.message], edit)
				}
			}
		}
		for path, byMessage := range edits {
			compared += compareGolden(test, path, byMessage)
		}
	}
	test.Logf("%d fixed files compared with their goldens", compared)
	if compared == 0 {
		test.Fatal("the port made no edit to the upstream fixtures, so the comparison proves nothing")
	}
}

// compareGolden fails the test unless the edits byMessage, applied to the file at path, make its golden as
// TestPortFixesMakeUpstreamGolden reads it, and is how many fixed files it compared.
func compareGolden(test *testing.T, path string, byMessage map[string][]offsetEdit) int {
	test.Helper()
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	golden, goldenError := os.ReadFile(path + ".golden")
	if goldenError != nil {
		test.Fatal(goldenError)
	}
	archive := txtar.Parse(golden)
	if len(archive.Files) == 0 {
		all := []offsetEdit{}
		for _, messageEdits := range byMessage {
			all = append(all, messageEdits...)
		}
		compareFixed(test, path, source, all, archive.Comment)
		return 1
	}
	if len(archive.Comment) != 0 {
		test.Fatalf("%s.golden holds both a leading comment and sections, which staticcheck refuses too", path)
	}
	compared := 0
	for message, messageEdits := range byMessage {
		index := slices.IndexFunc(archive.Files, func(section txtar.File) bool { return section.Name == message })
		if index < 0 {
			test.Errorf("%s.golden has no section for the port's fix %q", path, message)
			continue
		}
		want := append(bytes.TrimRight(archive.Files[index].Data, "\n"), '\n')
		compareFixed(test, path+"["+message+"]", source, messageEdits, want)
		compared++
	}
	for _, section := range archive.Files {
		if _, isMessage := byMessage[section.Name]; !isMessage {
			test.Errorf("%s.golden has a section for the fix %q, which the port does not suggest", path, section.Name)
		}
	}
	return compared
}

// compareFixed fails the test unless edits, applied to source from the last to the first and the result gofmt'd,
// make want; name says which file and fix it compares.
func compareFixed(test *testing.T, name string, source []byte, edits []offsetEdit, want []byte) {
	test.Helper()
	ordered := slices.Clone(edits)
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
	slices.SortFunc(ordered, func(first offsetEdit, second offsetEdit) int {
		return cmp.Compare(second.start, first.start)
	})
	fixed := slices.Clone(source)
	for index, each := range ordered {
		if index > 0 && each.end > ordered[index-1].start {
			test.Fatalf("%s: the port's edits overlap at offset %d", name, each.start)
		}
		fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
	}
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Fatalf("%s: the port's fixes make source gofmt cannot format: %v\n%s", name, formatError, fixed)
	}
	if !bytes.Equal(formatted, want) {
		test.Errorf("%s: the port's fixes make\n%s\nand the golden is\n%s", name, formatted, want)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every whence
// constant given first, through interfaces, pointers, embedded structs and interfaces, generic types and a type
// parameter, a renamed import, parentheses around the function and the arguments, arguments across lines, one call
// nested in another's arguments, calls under go, defer and a closure, and the calls the original leaves alone, a
// swapped signature, a named whence type, a field holding a function, a struct field spelled SeekStart, a method
// expression, a method value called later, a conversion and a constant expression, and a generated file, and their
// findings must be the same multiset of file, start, end, message and suggested fixes, each fix's message and each
// edit's file, offsets and text. Each side's count is logged, and the test fails if the original finds nothing.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, append(slices.Clone(upstreamFixtures), "parity")...)
	requireIdentical(test, original, port)
}

// adjudicatedPattern is one comment in the differences fixture, the text after its `// port ` opening: `finds:` and
// the message of a finding only the port makes, or `renames:`, the original's message, `=>` and the port's, for a
// finding both make in words that differ.
var adjudicatedPattern = regexp.MustCompile(`^(?:finds: (.+?)|renames: (.+?) => (.+?))\s*$`)

// Over the differences fixture the two may disagree only where each disagreement was adjudicated in the port's
// favour: every finding the port alone makes is recorded on its line, as found or as a renaming, every finding the
// original alone makes is the original's side of a recorded renaming, and every record is matched. A finding the
// original makes and the port does not, unrecorded, fails, since the port is never worse than the original. The
// fixture holds the shapes where the two could most plausibly part, a dot import beside the same package imported
// under a name, a Seek returning a named int64, and arguments the fix writes across lines and with a comment among
// them, and records no difference, since there is none, so the test holds that the two agree there, fixes included,
// and fails should the original find nothing in it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "differences")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the differences fixture, so the agreement proves nothing")
	}
	expectedPort := []string{}
	expectedOriginal := []string{}
	path := filepath.Join(analysistest.TestData(), "src", "differences", "differences.go")
	file, openError := os.Open(path)
	if openError != nil {
		test.Fatal(openError)
	}
	defer func() {
		_ = file.Close() // Ignored: the file was only read, and every line it holds has been read.
	}()
	fileScanner := bufio.NewScanner(file)
	for line := 1; fileScanner.Scan(); line++ {
		for _, comment := range strings.Split(fileScanner.Text(), "// port ")[1:] {
			match := adjudicatedPattern.FindStringSubmatch(comment)
			switch {
			case match == nil:
				test.Fatalf("differences.go:%d holds a comment the test cannot read: %q", line, comment)
			case match[1] != "":
				expectedPort = append(expectedPort, lineAndMessage("differences.go", line, match[1]))
			default:
				expectedOriginal = append(expectedOriginal, lineAndMessage("differences.go", line, match[2]))
				expectedPort = append(expectedPort, lineAndMessage("differences.go", line, match[3]))
			}
		}
	}
	if scanError := fileScanner.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	test.Logf("the fixture records %d differences", len(expectedPort))
	compareAdjudicated(test, "the port", linesAndMessages(only(port, original)), expectedPort)
	compareAdjudicated(test, "the original", linesAndMessages(only(original, port)), expectedOriginal)
}

// compareAdjudicated fails the test unless the findings side alone makes are exactly those the fixture records.
func compareAdjudicated(test *testing.T, side string, actual []string, expected []string) {
	test.Helper()
	for _, unrecorded := range difference(actual, expected) {
		test.Errorf("%s alone found %s, which the fixture does not record", side, unrecorded)
	}
	for _, unmatched := range difference(expected, actual) {
		test.Errorf("the fixture records that %s alone finds %s, and it does not", side, unmatched)
	}
}

// only is the findings of from that without lacks, compared whole, ends and fixes included, counting repeats.
func only(from []diagnostic, without []diagnostic) []diagnostic {
	remaining := map[string]int{}
	for _, found := range without {
		remaining[described(found)]++
	}
	alone := []diagnostic{}
	for _, found := range from {
		if remaining[described(found)] > 0 {
			remaining[described(found)]--
			continue
		}
		alone = append(alone, found)
	}
	return alone
}

// lineAndMessage is a finding as the differences fixture's comments name it, by its file, line and message.
func lineAndMessage(file string, line int, message string) string {
	return fmt.Sprintf("%s:%d: %s", file, line, message)
}

// linesAndMessages is every finding of found by its file, line and message alone, its columns, end and fixes dropped.
func linesAndMessages(found []diagnostic) []string {
	all := []string{}
	for _, each := range found {
		all = append(all, lineAndMessage(each.file, each.line, each.message))
	}
	return all
}

// requireIdentical fails the test unless original and port are the same multiset of findings, each with where it
// starts and ends and every fix and edit it suggests. Each side's count is logged, since two empty sides would match
// and prove nothing, and the test fails if the original finds nothing at all.
func requireIdentical(test *testing.T, original []diagnostic, port []diagnostic) {
	test.Helper()
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

// diagnostic is one finding as the tests compare it: where it starts and ends, its message and its fixes.
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

// offsetEdit is one edit of a fix, by its file and byte offsets in it, so it applies to the file's bytes.
type offsetEdit struct {
	file    string
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
			fmt.Fprintf(text, " %s[%d,%d) %q", edit.file, edit.start, edit.end, edit.newText)
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's SA1013 and the port over them in one
// checker, and returns each one's findings with their ends and fixes, sorted.
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
	// The packages, not the roots: a package with tests is analyzed once, as its test variant, which holds all its
	// files, as staticcheck's runner analyzes it, where the roots hold both and would report its files twice.
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa1013.Analyzer, Analyzer}, loaded.Packages, &checker.Options{},
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
					start := action.Package.Fset.Position(edit.Pos)
					each.edits = append(each.edits, offsetEdit{
						file:    filepath.Base(start.Filename),
						start:   start.Offset,
						end:     action.Package.Fset.Position(edit.End).Offset,
						newText: string(edit.NewText),
					})
				}
				found.fixes = append(found.fixes, each)
			}
			switch action.Analyzer {
			case sa1013.Analyzer:
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
