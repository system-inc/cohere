package struct_conversion_by_field_copy

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
	"honnef.co/go/tools/simple/s1016"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1016, which it keeps five times, under simple/s1016/testdata/go1.0,
// go1.7, go1.8, go1.9 and go1.18, each with its golden file, each copied unchanged into the module of its folder's
// name. S1016 asks for identical struct tags where the language version is older than go1.8, so the version matters:
// staticcheck's testutil loads each folder as a module whose go.mod, overlaid, says `go 1.N` for its folder's name,
// and the go line is what its loader and ours alike hand the type checker as the package's version, which the gate
// reads. So each testdata/src/go1.N is a module, example.com as staticcheck's is, whose go.mod says its folder's
// version, and every load checks that the version reached the package, so a fixture's silence is the gate's and not
// a version lost.
const upstream = "CheckSimplerStructConversion"

// versions are the modules the fixtures load from, each named for the version its go.mod says, staticcheck's five.
var versions = []string{"go1.0", "go1.7", "go1.8", "go1.9", "go1.18"}

// upstreamFiles is the one file of the upstream fixture under each version, the one its golden file fixes.
var upstreamFiles = map[string]string{
	"go1.0":  "convert.go",
	"go1.7":  "convert.go",
	"go1.8":  "convert.go",
	"go1.9":  "convert_alias.go",
	"go1.18": "convert_generics.go",
}

// parityPackages is the fixtures written for the comparison, by the module each loads from: under go1.18, parity,
// which holds every shape the check branches on and the near miss of each, a renamed import and a generated file, and
// its dependency other; under go1.7, versions, an untagged file the gate holds to identical tags and a tagged file the
// type checker gives go1.21.
var parityPackages = map[string][]string{
	"go1.7":  {"./versions"},
	"go1.18": {"./parity", "./parity/other"},
}

// differencesModule is the module whose differences fixture records where the port parts from the original.
const differencesModule = "go1.18"

// moduleEnvironment loads a fixture module as analysistest does, in module mode with nothing fetched, as the parity
// harness loads a fixture directory that holds a go.mod.
var moduleEnvironment = []string{"GO111MODULE=on", "GOPROXY=off", "GOWORK=off"}

// module is the directory of the fixture module of version.
func module(version string) string {
	return filepath.Join(analysistest.TestData(), "src", version)
}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under another name as readily as under S1016's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1016.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1016.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note the upstream fixture holds, under each version, the port reports on the same line with
// a message the note matches, and the port reports nothing no note expects. The go1.7 and go1.8 fixtures hold the
// same literal of a tagged struct, which only the go1.8 note expects, so the gate is what parts them.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		fixture := filepath.Join(module(version), upstream)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("under %s the upstream fixture holds no notes, so the comparison proves nothing", version)
		}
		// The load checks the package's version, and parity.Findings below loads the same module the same way.
		diagnosticsOf(test, version, "./"+upstream)
		original, port := parity.Findings(test, s1016.Analyzer, Analyzer, module(version), "./"+upstream)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("under %s the fixture expects %d, the original found %d, the port %d", version, len(wants),
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

// The port's fixes, applied to each version's upstream fixture, make its golden file, as staticcheck's testutil
// requires of the original's: the golden file is read as a txtar archive, whose comment is the whole fixed file when
// it holds no sections, and the fixed file is gofmt'd before it is compared.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		_, port := diagnosticsOf(test, version, "./"+upstream)
		path := filepath.Join(module(version), upstream, upstreamFiles[version])
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
			test.Fatalf("under %s the golden file holds %d sections, one a fix's message, which this test does not "+
				"read", version, len(archive.Files))
		}
		edits := []offsetEdit{}
		for _, diagnostic := range port {
			for _, fix := range diagnostic.fixes {
				edits = append(edits, fix.edits...)
			}
		}
		test.Logf("under %s the port found %d, with %d edits", version, len(port), len(edits))
		if len(edits) == 0 {
			test.Fatalf("under %s the port made no edit to the upstream fixture, so the comparison proves nothing",
				version)
		}
		// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
		slices.SortFunc(edits, func(first offsetEdit, second offsetEdit) int {
			return cmp.Compare(second.start, first.start)
		})
		fixed := slices.Clone(source)
		for index, each := range edits {
			if index > 0 && each.end > edits[index-1].start {
				test.Fatalf("under %s the port's edits overlap at offset %d", version, each.start)
			}
			fixed = slices.Concat(fixed[:each.start], []byte(each.newText), fixed[each.end:])
		}
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("under %s the port's fixes make source gofmt cannot format: %v\n%s", version, formatError,
				fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("under %s the port's fixes make\n%s\nand the golden file is\n%s", version, formatted,
				archive.Comment)
		}
	}
}

// The original and the port run in one checker over each version's upstream fixture and the parity fixtures of its
// module, and their findings must be the same multiset of file, line, column and message.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, version := range versions {
		patterns := slices.Concat([]string{"./" + upstream}, parityPackages[version])
		// The load checks every package's version, as parity.RequireIdentical loads the same module the same way.
		diagnosticsOf(test, version, patterns...)
		parity.RequireIdentical(test, s1016.Analyzer, Analyzer, module(version), patterns...)
	}
}

// Over every fixture of every version, each finding's end and every suggested fix, its message and each edit's start,
// end and new text, are the original's, which the parity harness does not compare yet. The fix's text is the
// conversion each builds around the literal's own type and the last element's identifier, printed by
// edit.ReplaceWithNode, so this is where the two are held byte for byte, literals across lines with comments among
// their elements, a renamed import, generic instances and aliases among them.
func TestPortFixesAndRangesMatchOriginal(test *testing.T) {
	test.Parallel()
	// What the port drops, recorded in the differences fixture, is held by TestPortDropsOnlyWhatIsRecorded; every
	// other finding of the original's, the port makes with the same end and fix.
	drops := recordedDrops(test)
	allOriginal := []diagnostic{}
	allPort := []diagnostic{}
	for _, version := range versions {
		original, port := diagnosticsOf(test, version, "./...")
		if version == differencesModule {
			original = slices.DeleteFunc(original, func(found diagnostic) bool {
				return slices.Contains(drops,
					parity.Finding{File: found.file, Line: found.line, Message: found.message})
			})
		}
		allOriginal = append(allOriginal, original...)
		allPort = append(allPort, port...)
	}
	originalDescribed := describedAll(allOriginal)
	portDescribed := describedAll(allPort)
	test.Logf("the original found %d, the port %d", len(originalDescribed), len(portDescribed))
	if len(originalDescribed) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	for _, each := range allOriginal {
		if len(each.fixes) != 1 {
			test.Fatalf("the original's finding %s carries %d fixes, where it always suggests one", described(each),
				len(each.fixes))
		}
	}
	for _, missing := range difference(originalDescribed, portDescribed) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range difference(portDescribed, originalDescribed) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
}

// The differences fixture holds the shapes where the port parts from the original, and those nearest them, where the
// two agree. The port parts in two ways, where the original is wrong: it drops a literal whose address is taken
// through parentheses, and a literal of the very type its fields are copied from, by an alias or by a generic
// instance another type checker made, each finding recorded on its line as `// port drops: <the original's message>`.
// The parity harness reads `finds:` and `renames:` alone in this base (#dcgn32d brings `drops:`), so this holds the
// records: the port finds nothing the original does not, and what the original alone finds is exactly what is
// recorded, line and message, with every record matched.
func TestPortDropsOnlyWhatIsRecorded(test *testing.T) {
	test.Parallel()
	diagnosticsOf(test, differencesModule, "./differences", "./differences/box")
	original, port := parity.Findings(test, s1016.Analyzer, Analyzer, module(differencesModule), "./differences",
		"./differences/box")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	for _, extra := range parity.Difference(port, original) {
		test.Errorf("the port alone found %s, which the fixture does not record", extra)
	}
	drops := recordedDrops(test)
	if len(drops) == 0 {
		test.Fatal("the differences fixture records no drop, so the comparison proves nothing")
	}
	if len(port) == 0 {
		test.Fatal("the port found nothing in the differences fixture, so its agreements prove nothing")
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
// no drop fails, since this fixture records nothing else, and so does a drop with no reason in a comment on the line
// above it.
func recordedDrops(test *testing.T) []parity.Finding {
	test.Helper()
	directory := filepath.Join(module(differencesModule), "differences")
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
		commentLines := map[int]bool{}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				commentLines[fileSet.Position(comment.Slash).Line] = true
			}
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				line := fileSet.Position(comment.Slash).Line
				for _, record := range strings.Split(comment.Text, "// port ")[1:] {
					message, isDrop := strings.CutPrefix(strings.TrimSpace(record), "drops: ")
					if !isDrop {
						test.Fatalf("%s:%d holds a record that is no drop: %q", filepath.Base(name), line, record)
					}
					if !commentLines[line-1] {
						test.Fatalf("%s:%d records a drop with no reason on the line above it", filepath.Base(name),
							line)
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

// diagnosticsOf loads the packages patterns name from the fixture module of version, runs staticcheck's S1016 and the
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
		[]*analysis.Analyzer{s1016.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case s1016.Analyzer:
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
