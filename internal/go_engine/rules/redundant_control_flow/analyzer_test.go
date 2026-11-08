package redundant_control_flow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"honnef.co/go/tools/simple/s1023"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_wants"
)

// upstream is staticcheck's own fixtures for S1023, one for each of its two judgments, copied unchanged from
// simple/s1023/testdata/go1.0/CheckRedundantBreak and CheckRedundantReturn into testdata/src under the same names. The
// go1.0 folder sets the language version of staticcheck's run, which S1023 never reads, so the fixtures load from a
// tree laid out by import path as the house's fixtures do. Neither has a golden file, since S1023 suggests no fix.
var upstream = []string{"CheckRedundantBreak", "CheckRedundantReturn"}

// Every `//@ diag(...)` note each upstream fixture holds, the port reports on the same line with a message the note
// matches, and the port reports nothing no note expects.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	for _, folder := range upstream {
		fixture := filepath.Join(analysistest.TestData(), "src", folder)
		wants, readError := staticcheck_wants.Read(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		if len(wants) == 0 {
			test.Fatalf("the upstream fixture %s holds no notes, so the comparison proves nothing", folder)
		}
		_, port := parity.Findings(test, s1023.Analyzer, Analyzer, analysistest.TestData(), folder)
		found := []staticcheck_wants.Finding{}
		for _, each := range port {
			// The wants name a file by its full path, and the findings by its base name, in the fixture's one folder.
			found = append(found, staticcheck_wants.Finding{
				File: filepath.Join(fixture, each.File), Line: each.Line, Message: each.Message,
			})
		}
		test.Logf("%s expects %d, the port found %d", folder, len(wants), len(found))
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

// The original and the port run in one checker over staticcheck's fixtures and parity's, the last holding every shape
// the check branches on and the near miss of each, and a generated file, and their findings must be the same multiset,
// each the same to its end and its fixes. S1023 suggests no fix, so this holds the port to suggesting none as well.
func TestPortMatchesOriginal(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1023.Analyzer, Analyzer, analysistest.TestData(),
		slices.Concat(upstream, []string{"parity"})...)
}

// The two shapes gofmt rewrites, an empty result list, which the parser gives a list of no fields where none gives
// nil, and an empty statement before a clause's break, are held the same way, ends and fixes too. The guard holds
// every .go file of the tree to gofmt, so the fixture is kept as testdata/unformatted/unformatted.go.txt and written
// out as Go into a tree of the test's own.
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
	parity.RequireIdentical(test, s1023.Analyzer, Analyzer, directory, "unformatted")
}

// The differences fixture holds the shapes where the two could most plausibly part, a generic type's method, a select
// clause around a switch, a literal ending a case clause and a clause ending a literal, a break in a block, a type
// switch that binds nothing, and function types among parameters and results, and records none, since the port parts
// from the original nowhere. The parity harness's RequireOnlyAdjudicated fails a fixture that records nothing, so the
// two are required to agree there finding for finding, ends and fixes too, in a call of its own, which fails when the
// original finds nothing, so the agreement is over findings.
func TestPortDiffersFromOriginalNowhere(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, s1023.Analyzer, Analyzer, analysistest.TestData(), "differences")
}
