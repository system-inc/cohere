package sliding_copy

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1018"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
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
		_, port := parity.Findings(test, s1018.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
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
		edits := []parity.Edit{}
		reported := 0
		for _, finding := range port {
			// Every fix of S1018 replaces the loop its finding is at, so a finding's file is its edits'.
			if finding.File != upstream.file {
				continue
			}
			reported++
			for _, fix := range finding.Fixes {
				edits = append(edits, fix.Edits...)
			}
		}
		test.Logf("in %s the port found %d, with %d edits", upstream.file, reported, len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", upstream.file)
		}
		// Applied from the last to the first, each edit's offsets still name the bytes they were read against.
		slices.SortFunc(edits, func(first parity.Edit, second parity.Edit) int {
			return cmp.Compare(second.Start, first.Start)
		})
		fixed := slices.Clone(source)
		for index, edit := range edits {
			if index > 0 && edit.End > edits[index-1].Start {
				test.Fatalf("the port's edits to %s overlap at offset %d", upstream.file, edit.Start)
			}
			fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
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
// be the same multiset, each the same to its end and its fixes. The original builds its fix through code.EditMatch
// from the identifiers its query binds, and the port through edit.ReplaceWithNode from the ones it binds, so this is
// where the bindings are held to the original's, their parentheses removed, and the loops replaced byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1018.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// In each package of the differences fixture, the port's findings are the original's but where a record says the
// port drops a finding, a counter left where it stood or one that is its own offset or limit, or withholds its fix,
// where copy names no builtin at the loop or the counter outlives it. Every shape no record is on, the counter named
// copy, a copy declared after the loop, identifiers across lines and nested loops, is the original's to the byte.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	for _, fixture := range differencesFixtures {
		test.Run(fixture, func(test *testing.T) {
			test.Parallel()
			parity.RequireOnlyAdjudicated(test, s1018.Analyzer, Analyzer, analysistest.TestData(), fixture)
		})
	}
}

// The pattern looks through parentheses around the loop's condition and doubled ones, and the parser leaves an empty
// statement after a store followed by two semicolons, shapes the house's testdata cannot hold, since its guard holds
// testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a
// tree of the test's own, where the two are held the same to the end and fixes of every finding.
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
