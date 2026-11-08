package manual_trim

import (
	"os"
	"path/filepath"
	"testing"

	"honnef.co/go/tools/simple/s1017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixture for S1017, copied unchanged from simple/s1017/testdata/go1.0/CheckTrim into
// testdata/src/CheckTrim. The go1.0 folder sets the language version of staticcheck's run, which S1017 never reads, so
// the fixture loads from a tree laid out by import path as the house's fixtures do. It holds no golden file, since
// S1017 suggests no fix.
const upstream = "CheckTrim"

// fixtures is every fixture package where the original and the port agree: staticcheck's; parity, which holds every
// shape the check branches on and the near miss of each, a package spelled strings at another path and a generated
// file; and packagelength, whose package scope declares a len of its own.
var fixtures = []string{upstream, "parity", "packagelength"}

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
	_, port := parity.Findings(test, s1017.Analyzer, Analyzer, analysistest.TestData(), upstream)
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

// Over staticcheck's fixture and parity's, the original and the port find the same multiset, each finding the same to
// its end, the whole if statement for a guarded call and its header for a guarded slice. S1017 suggests no fix, so a
// fix either side made would be a difference.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1017.Analyzer, Analyzer, analysistest.TestData(), fixtures...)
}

// gofmt strips the parentheses around an if statement's condition, which the original takes as written, so a
// parenthesized guard is held to the original's judgment here. The guard holds every .go file of the tree to gofmt,
// so the fixture is kept as testdata/unformatted/unformatted.go.txt and written out as Go into a tree of the test's
// own.
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
	parity.RequireIdentical(test, s1017.Analyzer, Analyzer, directory, "unformatted")
}

// The differences fixture holds the shapes where the two could most plausibly part, a type parameter of byte slices,
// a promoted field against its embedding's, literal offsets of float and rune kinds, else if chains around
// initializers, guards labeled and in clauses and literals, a dot import, and indexes of conversions, len and cap.
// The port parts from the original in one way, where the original is wrong (#zx3fz6w): it drops a guard on a
// selector or index whose syntax holds a call or a receive, each such finding recorded on its line as
// `// port drops: <the original's message>`. Every other finding the two make there is the same to its end.
func TestPortDiffersFromOriginalOnlyWhereRecorded(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, s1017.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// The one place the original crashes and the port declines: astutil.Equal panics on a node kind it does not know, a
// function type in an index, and on a field's tag absent on one side only, and a panic in a check is a crash under
// its rule's name. The original can't run over the panicking fixture, so the port runs alone, through analysistest,
// which fails on any finding there, as the fixture holds no want, and on the panic.
func TestPortDeclinesWhereTheOriginalPanics(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "panicking")
}
