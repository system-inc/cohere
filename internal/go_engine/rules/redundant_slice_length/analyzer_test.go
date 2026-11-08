package redundant_slice_length

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1010"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1010, copied unchanged from simple/s1010/testdata/go1.0/CheckSlicing
// into testdata/src/CheckSlicing with its golden file. The go1.0 folder sets the language version of staticcheck's
// run, which S1010 never reads, so the fixture loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSlicing"

// upstreamFile is the upstream fixture's one file, beside its golden file.
const upstreamFile = "slicing.go"

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
	_, port := parity.Findings(test, s1010.Analyzer, Analyzer, analysistest.TestData(), upstream)
	found := []staticcheck_wants.Finding{}
	for _, each := range port {
		// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
		found = append(found, staticcheck_wants.Finding{
			File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
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

// The port's fixes, applied to the upstream fixture, make its golden file byte for byte, as staticcheck's testutil
// requires of the original's.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	_, port := parity.Findings(test, s1010.Analyzer, Analyzer, analysistest.TestData(), upstream)
	path := filepath.Join(analysistest.TestData(), "src", upstream, upstreamFile)
	source, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	golden, goldenError := os.ReadFile(path + ".golden")
	if goldenError != nil {
		test.Fatal(goldenError)
	}
	edits := editsIn(port, upstreamFile)
	test.Logf("the port found %d, with %d edits", len(port), len(edits))
	if len(edits) == 0 {
		test.Fatal("the port made no edit to the upstream fixture, so the comparison proves nothing")
	}
	fixed := applied(test, upstreamFile, source, edits)
	if !bytes.Equal(fixed, golden) {
		test.Errorf("the port's fixes make\n%s\nand the golden file is\n%s", fixed, golden)
	}
}

// The original and the port run in one checker over staticcheck's fixture and parity's, the second holding every
// shape the query branches on and the near miss of each, and a generated file, and their findings must be the same
// multiset, each the same to its end and its fixes.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1010.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity")
}

// The differences fixture holds the shapes where the two could most plausibly part, a type parameter's slice, a
// string and a pointer to an array, a variable shadowed in an inner scope, a len declared by a package or a
// parameter, conversions and a function literal, and records none, since the port parts from the original nowhere.
// The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the two are required to agree
// there finding for finding, to each finding's end and its fixes, in a call of its own, which fails when the original
// finds nothing there.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1010.Analyzer, Analyzer, analysistest.TestData(), "differences",
		"differences/length")
}

// editsIn is every edit of every fix of findings in the file named file, by its base name.
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
