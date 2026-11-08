package nil_context_argument

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
	"honnef.co/go/tools/staticcheck/sa1012"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreamFixtures are staticcheck's own fixtures for SA1012, copied unchanged from staticcheck/sa1012/testdata into
// testdata/src with their golden files and their version folders kept, since both hold a package of one name: go1.0's,
// and go1.18's, whose calls instantiate generic functions. A version folder sets the language version of staticcheck's
// run, which SA1012 never reads, and a fixture loaded outside a module may use generics, so each loads from a tree laid
// out by import path as the house's fixtures do, the folder part of its path.
var upstreamFixtures = []string{"go1.0/checkStdlibUsageNilContext", "go1.18/checkStdlibUsageNilContext"}

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
			path := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(pattern), diagnostic.file)
			if edits[path] == nil {
				edits[path] = map[string][]offsetEdit{}
			}
			for _, fix := range diagnostic.fixes {
				edits[path][fix.message] = append(edits[path][fix.message], fix.edits...)
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

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding a nil
// context passed to functions, methods, promoted and interface methods, generic functions and the standard library's,
// through parentheses and instantiations, and the calls the original leaves alone, conversions, function variables, a
// method expression with no parameters and a shadowed nil, and a generated file, and their findings must be the same
// multiset of file, start, end, message and suggested fixes, each fix's message and each edit's offsets and text.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, append(slices.Clone(upstreamFixtures), "parity")...)
	requireIdentical(test, original, port)
}

// The differences fixture holds the shapes where the two could most plausibly part, and records none, since the port
// parts from the original nowhere, so the two are required to agree there finding for finding, fixes included, and the
// test fails if the original finds nothing, so the agreement is over findings and not over silence. It holds the
// original's false positive, a method expression whose receiver is the nil, and parameters a context only in part: an
// alias, an embedding interface and a constrained type parameter.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	original, port := diagnosticsOf(test, "differences")
	requireIdentical(test, original, port)
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

// diagnosticsOf loads the fixture packages patterns name, runs staticcheck's SA1012 and the port over them in one
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
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sa1012.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case sa1012.Analyzer:
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
