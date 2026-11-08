package single_append_concatenation

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1011"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1011, copied unchanged from simple/s1011/testdata/go1.0/CheckLoopAppend
// into testdata/src/CheckLoopAppend with its golden file. The go1.0 folder sets the language version of staticcheck's
// run, which S1011 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckLoopAppend"

// upstreamFile is the upstream fixture's one file, beside its golden file.
const upstreamFile = "loop-append.go"

// fixtures is every fixture package where the original and the port agree, fixes and all: staticcheck's; parity, which
// holds every shape the query's three alternatives and the checks after it branch on, the near miss of each, and a
// generated file, which both filter; shadow, which declares an append of its own at package scope; and walk, which
// holds every place the port's walk over the functions where syntax starts must reach, each once.
var fixtures = []string{upstream, "parity", "shadow", "walk"}

// differencesFixture is the fixture where the port parts from the original, each part recorded on its line, and the
// shapes where the two could most plausibly part and agree.
const differencesFixture = "differences"

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The harness keys each side by the analyzer itself, so it would hold
// a port under another name as readily as under S1011's; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1011.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1011.Analyzer.Name)
	}
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
	_, port := parity.Findings(test, s1011.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its path below testdata's src.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(each.File)), Line: each.Line,
			Message: each.Message,
		})
	}
	test.Logf("the fixture expects %d, the port found %d", len(wants), len(found))
	unexpected, untaken := staticcheck_wants.Compare(wants, found)
	for _, extra := range unexpected {
		test.Errorf("%s:%d: the port reported %q, which no note on the line expects", extra.File, extra.Line,
			extra.Message)
	}
	for _, missing := range untaken {
		test.Errorf("the port reported nothing matching %s", missing)
	}
}

// The port's fixes, applied to the upstream fixture and formatted, make its golden file byte for byte, as
// staticcheck's testutil requires of the original's: it reads the golden as a txtar archive, which with no sections is
// all comment, and runs format.Source on the fixed file before comparing.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := parity.Findings(test, s1011.Analyzer, Analyzer, analysistest.TestData(), upstream)
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
	edits := []parity.Edit{}
	for _, finding := range port {
		for _, fix := range finding.Fixes {
			for _, edit := range fix.Edits {
				if edit.File == upstream+"/"+upstreamFile {
					edits = append(edits, edit)
				}
			}
		}
	}
	test.Logf("the port found %d, with %d edits", len(port), len(edits))
	if len(edits) == 0 {
		test.Fatal("the port made no edit to the upstream fixture, so the comparison proves nothing")
	}
	// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
	slices.SortFunc(edits, func(first parity.Edit, second parity.Edit) int {
		return cmp.Compare(second.Start, first.Start)
	})
	fixed := slices.Clone(source)
	for index, edit := range edits {
		if index > 0 && edit.End > edits[index-1].Start {
			test.Fatalf("the port's edits overlap at offset %d", edit.Start)
		}
		fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
	}
	formatted, formatError := format.Source(fixed)
	if formatError != nil {
		test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
	}
	if !bytes.Equal(formatted, archive.Comment) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", formatted, archive.Comment)
	}
}

// Over staticcheck's fixture and the parity fixtures, the original and the port find the same multiset, each finding
// the same to its end, its related entries and its fixes, every edit's offsets and text.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1011.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// A slice ranged over in parentheses, which the pattern language looks through and its binding stores without, so
// both messages print it bare, is a shape gofmt rewrites. The guard holds every .go file of the tree to gofmt, so the
// fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's own,
// which the harness then loads as it loads testdata.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
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
	parity.RequireIdentical(test, s1011.Analyzer, Analyzer, directory, "unformatted")
}

// Over the differences fixture, the port's findings are the original's but where a record says otherwise: the port
// drops a loop whose append spreads its element, in each of the three alternatives, and withholds the fix of a loop
// whose key or value is a variable declared outside the for statement, read after the loop or not, a parameter or
// another package's through a dot import. Every shape no record is on, a loop variable shadowed in the body, a slice
// that is ranged over and appended to, an element held in a variable spelled like its slice, a key the header defines
// and a loop in a function literal, must be the original's to the byte.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1011.Analyzer, Analyzer, analysistest.TestData(), differencesFixture)
}
