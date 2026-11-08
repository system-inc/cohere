package loop_copy

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1001"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreams are staticcheck's own fixtures for S1001, each copied unchanged, golden file and all, from
// simple/s1001/testdata/go1.0/CheckLoopCopy and go1.18/CheckLoopCopy into testdata/src under the same two folders,
// since the two fixtures share one folder name. The go1.N folder sets the language version of staticcheck's run,
// which S1001 never reads, gating nothing, so the fixtures load from a tree laid out by import path as the house's
// fixtures do, where the generic one type-checks under the newest language.
var upstreams = []struct {
	pattern string
	file    string
}{
	{pattern: "go1.0/CheckLoopCopy", file: "copy.go"},
	{pattern: "go1.18/CheckLoopCopy", file: "copy_generics.go"},
}

// fixtures is every fixture package the tree holds where the two agree outside the differences fixture,
// staticcheck's and the port's own: parity's and its package that declares len and copy at package scope.
var fixtures = []string{"go1.0/CheckLoopCopy", "go1.18/CheckLoopCopy", "parity", "parity/shadowed"}

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
		original, port := parity.Findings(test, s1001.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its path below testdata's src.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(each.File)), Line: each.Line,
				Message: each.Message,
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
		_, port := parity.Findings(test, s1001.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
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
		edits := editsIn(port, upstream.pattern+"/"+upstream.file)
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

// The original and the port run in one checker over staticcheck's fixtures and parity's, the second holding every
// shape the query and the judgment branch on and the near miss of each, a generated file, and a package that declares
// len and copy at package scope, and their findings must be the same multiset, each the same to its end and its
// fixes. The fixes are built from what the query binds, through edit.ReplaceWithNode on both sides, so this is where
// the bindings are held to the original's, their parentheses removed, and the printed loops replaced byte for byte.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The differences fixture holds the shapes where the two could most plausibly part, identifiers with no object in a
// function literal the destination holds, dereferences the fixes print, sources the same only by syntax and calls
// ranged over twice, and a key spelled like an outer variable, and records none, since the port parts from the
// original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are
// required to agree there finding for finding, fixes and all, in a call of its own, which fails when the original
// finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The pattern looks through parentheses around a ranged source and doubled ones, and the parser leaves an empty
// statement after a store followed by two semicolons, shapes the house's testdata cannot hold, since its guard holds
// testdata to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a
// tree of the test's own. The two are held the same there to each finding's end and its fixes, the
// gofmt-rewritable source's printed loops included.
func TestPortMatchesOriginalWhereGofmtWouldRewrite(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1001.Analyzer, Analyzer, unformattedTree(test), "unformatted")
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

// editsIn is every edit of every fix of findings in the file named file, by its path below testdata's src.
func editsIn(findings []parity.Finding, file string) []parity.Edit {
	edits := []parity.Edit{}
	for _, finding := range findings {
		for _, fix := range finding.Fixes {
			for _, edit := range fix.Edits {
				if edit.File == file {
					edits = append(edits, edit)
				}
			}
		}
	}
	return edits
}

// applied is source with edits applied, from the last to the first, so each edit's offsets still name the bytes they
// were read against. Two edits that overlap fail the test.
func applied(test *testing.T, file string, source []byte, edits []parity.Edit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(first parity.Edit, second parity.Edit) int {
		return cmp.Compare(second.Start, first.Start)
	})
	fixed := slices.Clone(source)
	for index, edit := range ordered {
		if index > 0 && edit.End > ordered[index-1].Start {
			test.Fatalf("the edits to %s overlap at offset %d", file, edit.Start)
		}
		fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
	}
	return fixed
}
