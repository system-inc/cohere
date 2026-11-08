package buffer_string_conversion

import (
	"bytes"
	"cmp"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/txtar"
	"honnef.co/go/tools/simple/s1030"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstreams are staticcheck's own fixtures for S1030, each copied unchanged, golden file and all, from
// simple/s1030/testdata/go1.0/CheckBytesBufferConversions and go1.9/CheckBytesBufferConversions into testdata/src
// under the same two folders, since the two fixtures share one folder name. The go1.N folder sets the language version
// of staticcheck's run, which S1030 never reads, gating nothing, so the fixtures load from a tree laid out by import
// path as the house's fixtures do, where the aliases of the go1.9 one type-check under the newest language.
var upstreams = []struct {
	pattern string
	file    string
}{
	{pattern: "go1.0/CheckBytesBufferConversions", file: "LintBytesBufferConversions.go"},
	{pattern: "go1.9/CheckBytesBufferConversions", file: "LintBytesBufferConversions.go"},
}

// fixtures is every fixture package the tree holds where the original and the port agree, fixes and all:
// staticcheck's two; parity, which holds every shape the query and the judgment branch on and the near miss of each,
// and a generated file, which both filter; parity/shadowed, which declares byte and string at package scope; and
// bytes_test, the path of bytes' external tests, which both skip.
var fixtures = []string{
	"go1.0/CheckBytesBufferConversions", "go1.9/CheckBytesBufferConversions", "parity", "parity/shadowed",
	"bytes_test",
}

// The port carries the original's name, so the rule, its settings and every `//cohere:ignore` naming it read the
// same once the port takes the original's place. The findings tests key each side by the analyzer itself, so they
// would hold a port under any name; this is what holds the name.
func TestPortCarriesTheOriginalsName(test *testing.T) {
	test.Parallel()
	if Analyzer.Name != s1030.Analyzer.Name {
		test.Errorf("the port is named %q, and the original %q", Analyzer.Name, s1030.Analyzer.Name)
	}
}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		fixture := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstream.pattern))
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("the upstream fixture %s holds no notes, so the comparison proves nothing", upstream.pattern)
		}
		original, port := parity.Findings(test, s1030.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
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
// sections, and the fixed file is gofmt'd before it is compared, which is what aligns the fixed lines' comments. Each
// finding suggests one fix, and neither golden file holds sections.
func TestPortFixesMakeUpstreamGolden(test *testing.T) {
	test.Parallel()
	for _, upstream := range upstreams {
		_, port := parity.Findings(test, s1030.Analyzer, Analyzer, analysistest.TestData(), upstream.pattern)
		path := filepath.Join(analysistest.TestData(), "src", filepath.FromSlash(upstream.pattern), upstream.file)
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
				upstream.pattern, len(archive.Files))
		}
		edits := editsIn(port, upstream.pattern+"/"+upstream.file)
		test.Logf("in %s the port found %d, with %d edits", upstream.pattern, len(port), len(edits))
		if len(edits) == 0 {
			test.Fatalf("the port made no edit to %s, so the comparison proves nothing", upstream.pattern)
		}
		fixed := applied(test, upstream.pattern, source, edits)
		formatted, formatError := format.Source(fixed)
		if formatError != nil {
			test.Fatalf("the port's fixes make source gofmt cannot format: %v\n%s", formatError, fixed)
		}
		if !bytes.Equal(formatted, archive.Comment) {
			test.Errorf("the port's fixes to %s make\n%s\nand the golden file is\n%s", upstream.pattern, formatted,
				archive.Comment)
		}
	}
}

// Over staticcheck's fixtures and the parity fixtures, the original and the port find the same multiset, each
// finding the same to its end and its fixes. The fix's text is the one the original builds through
// edit.ReplaceWithPattern and the port through edit.ReplaceWithNode, so this is where the two are held byte for byte,
// a receiver in parentheses, across lines and with comments beside it included.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1030.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// The differences fixture holds the shapes where the two could most plausibly part, holders that embed a buffer
// beside methods of its names, receivers across lines and with comments beside them, and conversions nested in
// conversions, calls and function literals. The port's findings there are the original's, to their ends and fixes,
// but where a record says the port drops one, a conversion whose receiver's other method, the one the message asks
// for, is not the buffer's own.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1030.Analyzer, Analyzer, analysistest.TestData(), "differences")
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
// were read against. Two edits that overlap fail the test, naming fixture.
func applied(test *testing.T, fixture string, source []byte, edits []parity.Edit) []byte {
	test.Helper()
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(first parity.Edit, second parity.Edit) int {
		return cmp.Compare(second.Start, first.Start)
	})
	fixed := slices.Clone(source)
	for index, edit := range ordered {
		if index > 0 && edit.End > ordered[index-1].Start {
			test.Fatalf("the port's edits to %s overlap at offset %d", fixture, edit.Start)
		}
		fixed = slices.Concat(fixed[:edit.Start], []byte(edit.NewText), fixed[edit.End:])
	}
	return fixed
}
